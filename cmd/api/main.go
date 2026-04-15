package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/joho/godotenv"
	"github.com/project_radeon/api/internal/auth"
	"github.com/project_radeon/api/internal/connections"
	"github.com/project_radeon/api/internal/discovery"
	"github.com/project_radeon/api/internal/events"
	"github.com/project_radeon/api/internal/feed"
	"github.com/project_radeon/api/internal/interests"
	"github.com/project_radeon/api/internal/messages"
	"github.com/project_radeon/api/internal/user"
	"github.com/project_radeon/api/pkg/database"
	"github.com/project_radeon/api/pkg/middleware"
)

func main() {
	// Load .env in development
	godotenv.Load()

	db, err := database.Connect()
	if err != nil {
		log.Fatalf("database connection failed: %v", err)
	}
	defer db.Close()
	log.Println("connected to database")

	// Initialise handlers
	// discoveryHandler is created first — it is passed as a dependency to
	// interestsHandler (vector rebuild) and userHandler (cache invalidation).
	discoveryHandler := discovery.NewHandler(db)
	authHandler := auth.NewHandler(db)
	userHandler := user.NewHandler(db, discoveryHandler)
	feedHandler := feed.NewHandler(db)
	connectionHandler := connections.NewHandler(db)
	eventsHandler := events.NewHandler(db)
	messagesHandler := messages.NewHandler(db)
	interestsHandler := interests.NewHandler(db, discoveryHandler)

	r := chi.NewRouter()

	// Global middleware
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
		r.Get("/users/discover", userHandler.Discover)
		r.Get("/users/{id}", userHandler.GetUser)
		r.Put("/users/me/interests", interestsHandler.SetUserInterests)

		// Connections
		r.Post("/connections", connectionHandler.SendRequest)
		r.Get("/connections", connectionHandler.ListConnections)
		r.Get("/connections/pending", connectionHandler.ListPending)
		r.Patch("/connections/{id}", connectionHandler.UpdateStatus)
		r.Delete("/connections/{id}", connectionHandler.RemoveConnection)

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

		// Interests
		r.Get("/interests", interestsHandler.ListInterests)

		// Discovery
		r.Get("/users/suggestions", discoveryHandler.GetSuggestions)
		r.Post("/users/{id}/dismiss", discoveryHandler.DismissUser)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("project_radeon api running on :%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
