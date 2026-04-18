package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

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
	"github.com/project_radeon/api/internal/follows"
	"github.com/project_radeon/api/internal/meetups"
	"github.com/project_radeon/api/internal/user"
	"github.com/project_radeon/api/pkg/database"
	"github.com/project_radeon/api/pkg/middleware"
	"github.com/project_radeon/api/pkg/storage"
)

// main loads infrastructure dependencies, wires the HTTP handlers, and starts
// the API server.
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

	// The API only needs a single S3 uploader, so main wires the AWS client once
	// and passes the thin abstraction into the user handler.
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
	meetupsHandler := meetups.NewHandler(db)
	chatsHandler := chats.NewHandler(db)
	followsHandler := follows.NewHandler(db)

	r := chi.NewRouter()

	// Global middleware is applied before route grouping so both public and
	// authenticated endpoints get request IDs, panic recovery, and CORS headers.
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

		// Meetups
		r.Get("/meetups", meetupsHandler.ListMeetups)
		r.Post("/meetups", meetupsHandler.CreateMeetup)
		r.Get("/meetups/{id}", meetupsHandler.GetMeetup)
		r.Post("/meetups/{id}/rsvp", meetupsHandler.RSVP)
		r.Get("/meetups/{id}/attendees", meetupsHandler.GetAttendees)

		// Chats
		r.Get("/chats", chatsHandler.ListChats)
		r.Post("/chats", chatsHandler.CreateChat)
		r.Get("/chats/{id}/messages", chatsHandler.GetMessages)
		r.Post("/chats/{id}/messages", chatsHandler.SendMessage)
		r.Get("/chats/requests", chatsHandler.ListChatRequests)
		r.Patch("/chats/{id}/status", chatsHandler.UpdateChatStatus)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("project_radeon api running on :%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
