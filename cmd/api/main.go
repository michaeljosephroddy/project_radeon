package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/joho/godotenv"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/project_radeon/api/internal/auth"
	"github.com/project_radeon/api/internal/events"
	"github.com/project_radeon/api/internal/feed"
	"github.com/project_radeon/api/internal/follows"
	"github.com/project_radeon/api/internal/messages"
	"github.com/project_radeon/api/internal/user"
	"github.com/project_radeon/api/pkg/database"
	"github.com/project_radeon/api/pkg/middleware"
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

	authHandler := auth.NewHandler(db)
	userHandler := user.NewHandler(db, uploader)
	feedHandler := feed.NewHandler(db)
	eventsHandler := events.NewHandler(db)
	messagesHandler := messages.NewHandler(db)
	followsHandler := follows.NewHandler(db)

	r := chi.NewRouter()

	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.RequestID)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: false,
	}))

	// ── Public routes ──────────────────────────────────────────────
	r.Post("/auth/register", authHandler.Register)
	r.Post("/auth/login", authHandler.Login)

	// ── Protected routes ───────────────────────────────────────────
	r.Group(func(r chi.Router) {
		r.Use(middleware.Authenticate)

		// Feed
		r.Get("/feed", feedHandler.GetFeed)

		// Posts
		r.Post("/posts", feedHandler.CreatePost)
		r.Delete("/posts/{id}", feedHandler.DeletePost)
		r.Post("/posts/{id}/react", feedHandler.ReactToPost)
		r.Get("/posts/{id}/reactions", feedHandler.GetReactions)
		r.Post("/posts/{id}/comments", feedHandler.AddComment)
		r.Get("/posts/{id}/comments", feedHandler.GetComments)

		// Users
		r.Get("/users/me", userHandler.GetMe)
		r.Patch("/users/me", userHandler.UpdateMe)
		r.Post("/users/me/avatar", userHandler.UploadAvatar)
		r.Get("/users/me/following", followsHandler.ListFollowing)
		r.Get("/users/me/followers", followsHandler.ListFollowers)
		r.Get("/users/discover", userHandler.Discover)
		r.Get("/users/{id}/posts", feedHandler.GetUserPosts)
		r.Get("/users/{id}", userHandler.GetUser)
		r.Post("/users/{id}/follow", followsHandler.Follow)
		r.Delete("/users/{id}/follow", followsHandler.Unfollow)

		// Events
		r.Get("/events", eventsHandler.ListEvents)
		r.Post("/events", eventsHandler.CreateEvent)
		r.Get("/events/{id}", eventsHandler.GetEvent)
		r.Post("/events/{id}/rsvp", eventsHandler.RSVP)
		r.Get("/events/{id}/attendees", eventsHandler.GetAttendees)

		// Messages
		r.Get("/conversations", messagesHandler.ListConversations)
		r.Post("/conversations", messagesHandler.CreateConversation)
		r.Get("/conversations/{id}/messages", messagesHandler.GetMessages)
		r.Post("/conversations/{id}/messages", messagesHandler.SendMessage)
		r.Get("/conversations/requests", messagesHandler.ListMessageRequests)
		r.Patch("/conversations/{id}/status", messagesHandler.UpdateConversationStatus)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("project_radeon api running on :%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
