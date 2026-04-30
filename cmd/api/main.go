package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/joho/godotenv"
	"github.com/project_radeon/api/internal/auth"
	"github.com/project_radeon/api/internal/chats"
	"github.com/project_radeon/api/internal/feed"
	"github.com/project_radeon/api/internal/friends"
	"github.com/project_radeon/api/internal/meetups"
	"github.com/project_radeon/api/internal/notifications"
	"github.com/project_radeon/api/internal/support"
	"github.com/project_radeon/api/internal/user"
	"github.com/project_radeon/api/pkg/cache"
	"github.com/project_radeon/api/pkg/database"
	"github.com/project_radeon/api/pkg/middleware"
	"github.com/project_radeon/api/pkg/observability"
	"github.com/project_radeon/api/pkg/response"
	"github.com/project_radeon/api/pkg/storage"
)

func main() {
	godotenv.Load()

	db, err := database.Connect()
	if err != nil {
		log.Fatalf("database connection failed: %v", err)
	}
	defer db.Close()
	log.Println("connected to database")

	awsRegion := strings.TrimSpace(os.Getenv("AWS_REGION"))
	awsBucket := strings.TrimSpace(os.Getenv("AWS_S3_BUCKET"))

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(awsRegion),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			strings.TrimSpace(os.Getenv("AWS_ACCESS_KEY_ID")),
			strings.TrimSpace(os.Getenv("AWS_SECRET_ACCESS_KEY")),
			"",
		)),
	)
	if err != nil {
		log.Fatalf("failed to load AWS config: %v", err)
	}
	s3Client := s3.NewFromConfig(awsCfg)
	uploader := storage.NewS3Uploader(s3Client, awsBucket, awsRegion)

	cacheEnabled := parseBoolEnv("CACHE_ENABLED")
	cacheStore, err := cache.New(context.Background(), cache.Config{
		Enabled:  cacheEnabled,
		Addr:     strings.TrimSpace(os.Getenv("REDIS_ADDR")),
		Password: os.Getenv("REDIS_PASSWORD"),
		DB:       parseIntEnv("REDIS_DB"),
		TLS:      parseBoolEnv("REDIS_TLS"),
		Prefix:   strings.TrimSpace(os.Getenv("REDIS_PREFIX")),
	})
	if err != nil {
		log.Fatalf("cache initialization failed: %v", err)
	}
	if cacheStore.Enabled() {
		log.Println("connected to redis cache")
	} else {
		log.Println("redis cache disabled")
	}

	userChecker := middleware.NewPGUserChecker(db)
	authHandler := auth.NewHandler(auth.NewPgStore(db))
	userStore := user.NewCachedStore(user.NewPgStoreWithConfig(db, user.StoreConfig{
		DiscoverPipelineV2: parseBoolEnvWithDefault("DISCOVER_PIPELINE_V2", true),
	}), cacheStore)
	feedStore := feed.NewCachedStore(feed.NewPgStore(db), cacheStore)
	meetupsStore := meetups.NewCachedStore(meetups.NewPgStoreWithConfig(db, meetups.StoreConfig{
		RecommendedPipelineV2: parseBoolEnvWithDefault("MEETUPS_RECOMMENDER_V2", true),
	}), cacheStore)
	supportStore := support.NewCachedStore(support.NewPgStore(db), cacheStore)
	friendsStore := friends.NewCachedStore(friends.NewPgStore(db), cacheStore)

	userHandler := user.NewHandler(userStore, uploader)
	notificationsService := notifications.NewService(
		notifications.NewPgStore(db),
		notifications.NewExpoProvider(nil),
	)
	notificationsHandler := notifications.NewHandler(notificationsService)
	chatsRealtimeHub := chats.NewRealtimeHub()
	chatsRealtimeBus := chats.NewRedisRealtimeBus(cacheStore)
	chatsHandler := chats.NewHandlerWithRealtimeInfra(chats.NewPgStore(db), notificationsService, chatsRealtimeHub, chatsRealtimeBus)
	friendsHandler := friends.NewHandler(friendsStore)
	feedHandler := feed.NewHandlerWithNotifier(feedStore, notificationsService, uploader)
	meetupsHandler := meetups.NewHandler(meetupsStore, uploader)
	supportHandler := support.NewHandlerWithChatBroadcaster(supportStore, chatsHandler)

	r := chi.NewRouter()

	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.RequestID)
	r.Use(observability.Middleware)
	r.Use(middleware.RateLimitIPWithStore(cacheStore))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: false,
	}))

	// Health check for ALB target group — must respond 200 before the instance
	// receives live traffic and during graceful drain checks.
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(r.Context()); err != nil {
			response.Error(w, http.StatusServiceUnavailable, "database unavailable")
			return
		}
		response.Success(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/debug/observability", observability.Handler(db, cacheStore.Enabled()))

	r.Group(func(r chi.Router) {
		r.Use(middleware.AuthenticateWebSocket)
		r.Use(middleware.EnsureCurrentUserExists(userChecker))
		r.Use(middleware.RateLimitUserWithStore(cacheStore))

		r.Get("/chats/ws", chatsHandler.ConnectRealtime)
	})

	apiRouter := chi.NewRouter()
	apiRouter.Use(chimiddleware.Timeout(30 * time.Second))

	// ── Public routes ──────────────────────────────────────────────
	apiRouter.Post("/auth/register", authHandler.Register)
	apiRouter.Post("/auth/login", authHandler.Login)
	apiRouter.Get("/interests", userHandler.ListInterests)

	// ── Protected routes ───────────────────────────────────────────
	apiRouter.Group(func(r chi.Router) {
		r.Use(middleware.Authenticate)
		r.Use(middleware.EnsureCurrentUserExists(userChecker))
		r.Use(middleware.RateLimitUserWithStore(cacheStore))

		// Feed
		r.Get("/feed/home", feedHandler.GetHomeFeed)
		r.Get("/feed/hidden", feedHandler.GetHiddenFeedItems)
		r.Post("/feed/items/{id}/react", feedHandler.ReactToFeedItem)
		r.Post("/feed/items/{id}/comments", feedHandler.AddFeedItemComment)
		r.Get("/feed/items/{id}/comments", feedHandler.GetFeedItemComments)
		r.Post("/feed/items/{id}/hide", feedHandler.HideFeedItem)
		r.Delete("/feed/items/{id}/hide", feedHandler.UnhideFeedItem)
		r.Post("/feed/authors/{id}/mute", feedHandler.MuteFeedAuthor)
		r.Post("/feed/impressions", feedHandler.LogFeedImpressions)
		r.Post("/feed/events", feedHandler.LogFeedEvents)

		// Posts
		r.Post("/posts", feedHandler.CreatePost)
		r.Post("/posts/images", feedHandler.UploadPostImage)
		r.Delete("/posts/{id}", feedHandler.DeletePost)
		r.Post("/posts/{id}/share", feedHandler.SharePost)
		r.Post("/posts/{id}/react", feedHandler.ReactToPost)
		r.Get("/posts/{id}/reactions", feedHandler.GetReactions)
		r.Post("/posts/{id}/comments", feedHandler.AddComment)
		r.Get("/posts/{id}/comments", feedHandler.GetComments)

		// Users
		r.Get("/users/me", userHandler.GetMe)
		r.Patch("/users/me", userHandler.UpdateMe)
		r.Patch("/users/me/location", userHandler.UpdateMyCurrentLocation)
		r.Post("/users/me/avatar", userHandler.UploadAvatar)
		r.Post("/users/me/banner", userHandler.UploadBanner)
		r.Get("/users/me/meetups", meetupsHandler.ListMyMeetups)
		r.Get("/users/me/friends", friendsHandler.ListFriends)
		r.Get("/users/me/friend-requests/incoming", friendsHandler.ListIncomingRequests)
		r.Get("/users/me/friend-requests/outgoing", friendsHandler.ListOutgoingRequests)
		r.Get("/users/discover/preview", userHandler.DiscoverPreview)
		r.Get("/users/discover", userHandler.Discover)
		r.Get("/users/{id}/posts", feedHandler.GetUserPosts)
		r.Get("/users/{id}", userHandler.GetUser)
		r.Post("/users/{id}/friend-request", friendsHandler.SendRequest)
		r.Patch("/users/{id}/friend-request", friendsHandler.UpdateRequest)
		r.Delete("/users/{id}/friend-request", friendsHandler.CancelRequest)
		r.Delete("/users/{id}/friend", friendsHandler.RemoveFriend)

		// Meetups
		r.Get("/meetups/categories", meetupsHandler.ListCategories)
		r.Get("/meetups", meetupsHandler.ListMeetups)
		r.Post("/meetups", meetupsHandler.CreateMeetup)
		r.Post("/meetups/images", meetupsHandler.UploadCoverImage)
		r.Get("/meetups/{id}", meetupsHandler.GetMeetup)
		r.Patch("/meetups/{id}", meetupsHandler.UpdateMeetup)
		r.Delete("/meetups/{id}", meetupsHandler.DeleteMeetup)
		r.Post("/meetups/{id}/publish", meetupsHandler.PublishMeetup)
		r.Post("/meetups/{id}/cancel", meetupsHandler.CancelMeetup)
		r.Post("/meetups/{id}/rsvp", meetupsHandler.RSVP)
		r.Get("/meetups/{id}/attendees", meetupsHandler.GetAttendees)
		r.Get("/meetups/{id}/waitlist", meetupsHandler.GetWaitlist)

		// Support
		r.Post("/support/requests", supportHandler.CreateSupportRequest)
		r.Get("/support/requests", supportHandler.ListSupportRequests)
		r.Get("/support/requests/mine", supportHandler.ListMySupportRequests)
		r.Get("/support/requests/{id}", supportHandler.GetSupportRequest)
		r.Patch("/support/requests/{id}", supportHandler.UpdateSupportRequest)
		r.Post("/support/requests/{id}/replies", supportHandler.CreateSupportReply)
		r.Get("/support/requests/{id}/replies", supportHandler.ListSupportReplies)
		r.Post("/support/requests/{id}/offers", supportHandler.CreateSupportOffer)
		r.Get("/support/requests/{id}/offers", supportHandler.ListSupportOffers)
		r.Post("/support/requests/{id}/offers/{offerId}/accept", supportHandler.AcceptSupportOffer)
		r.Post("/support/requests/{id}/offers/{offerId}/decline", supportHandler.DeclineSupportOffer)
		r.Post("/support/requests/{id}/offers/{offerId}/cancel", supportHandler.CancelSupportOffer)

		// Chats
		r.Get("/chats", chatsHandler.ListChats)
		r.Get("/chats/requests", chatsHandler.ListChatRequests)
		r.Post("/chats", chatsHandler.CreateChat)
		r.Get("/chats/{id}", chatsHandler.GetChat)
		r.Get("/chats/{id}/messages", chatsHandler.GetMessages)
		r.Post("/chats/{id}/messages", chatsHandler.SendMessage)
		r.Post("/chats/{id}/read", chatsHandler.MarkRead)
		r.Delete("/chats/{id}", chatsHandler.DeleteChat)
		r.Patch("/chats/{id}/status", chatsHandler.UpdateChatStatus)

		// Notifications
		r.Post("/notifications/devices", notificationsHandler.RegisterDevice)
		r.Delete("/notifications/devices/{id}", notificationsHandler.DeleteDevice)
		r.Get("/notifications", notificationsHandler.ListNotifications)
		r.Get("/notifications/summary", notificationsHandler.GetSummary)
		r.Post("/notifications/read", notificationsHandler.MarkNotificationsRead)
		r.Post("/notifications/read-all", notificationsHandler.MarkAllNotificationsRead)
		r.Post("/notifications/{id}/read", notificationsHandler.MarkNotificationRead)
		r.Get("/notifications/preferences", notificationsHandler.GetPreferences)
		r.Patch("/notifications/preferences", notificationsHandler.UpdatePreferences)
	})

	r.Mount("/", apiRouter)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Catch SIGTERM (ALB scale-in / ECS task stop) and SIGINT (local Ctrl-C).
	// srv.Shutdown stops accepting new connections and waits for in-flight
	// requests to finish before the process exits.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	workerCtx, stopWorker := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stopWorker()

	go func() {
		<-quit
		log.Println("shutting down — draining in-flight requests")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Fatalf("forced shutdown: %v", err)
		}
	}()

	go notifications.RunWorker(workerCtx, log.Default(), notificationsService, 15*time.Second, 25)
	go feed.RunAggregateRefreshWorker(
		workerCtx,
		log.Default(),
		db,
		time.Duration(parseIntEnvWithDefault("FEED_AGGREGATE_WORKER_POLL_MS", 2000))*time.Millisecond,
		parseIntEnvWithDefault("FEED_AGGREGATE_WORKER_BATCH_SIZE", 200),
	)
	if err := chatsRealtimeBus.Start(workerCtx, chatsRealtimeHub); err != nil {
		log.Fatalf("chat realtime bus failed: %v", err)
	}

	fmt.Printf("project_radeon api running on :%s\n", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
	log.Println("server stopped")
}

func parseBoolEnv(key string) bool {
	return parseBoolEnvWithDefault(key, false)
}

func parseBoolEnvWithDefault(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	enabled, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return enabled
}

func parseIntEnv(key string) int {
	return parseIntEnvWithDefault(key, 0)
}

func parseIntEnvWithDefault(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
