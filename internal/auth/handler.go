package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/project_radeon/api/pkg/response"
	"golang.org/x/crypto/bcrypt"
)

// Querier is the database interface required by the auth handler.
type Querier interface {
	EmailExists(ctx context.Context, email string) (bool, error)
	UsernameExists(ctx context.Context, username string) (bool, error)
	CreateUser(ctx context.Context, username, email, passwordHash, city, country string, soberSince *time.Time) (uuid.UUID, error)
	GetUserCredentials(ctx context.Context, email string) (id uuid.UUID, passwordHash string, err error)
}

type Handler struct {
	db Querier
}

// NewHandler builds an auth handler. Pass auth.NewPgStore(pool) for production.
func NewHandler(db Querier) *Handler {
	return &Handler{db: db}
}

// Register validates signup input, creates the user record, and returns a JWT.
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var input registerInput

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	input = normalizeRegisterInput(input)
	errs := validateRegisterInput(input)
	if len(errs) > 0 {
		response.ValidationError(w, errs)
		return
	}

	soberSince, err := parseSoberSince(input.SoberSince)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "sober_since must be YYYY-MM-DD")
		return
	}

	// Uniqueness checks stay explicit so the handler can return field-specific
	// API errors instead of a generic database constraint failure.
	emailExists, err := h.db.EmailExists(r.Context(), input.Email)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not validate email")
		return
	}
	if emailExists {
		response.Error(w, http.StatusConflict, "email already registered")
		return
	}

	usernameExists, err := h.db.UsernameExists(r.Context(), input.Username)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not validate username")
		return
	}
	if usernameExists {
		response.Error(w, http.StatusConflict, "username already taken")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not hash password")
		return
	}

	userID, err := h.db.CreateUser(r.Context(), input.Username, input.Email, string(hash), input.City, input.Country, soberSince)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not create user")
		return
	}

	token, err := GenerateToken(userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not generate token")
		return
	}

	response.Success(w, http.StatusCreated, map[string]any{
		"token":   token,
		"user_id": userID,
	})
}

// Login verifies email and password credentials and returns a fresh JWT.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var input loginInput

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	input = normalizeLoginInput(input)

	userID, passwordHash, err := h.db.GetUserCredentials(r.Context(), input.Email)
	if err != nil {
		// Don't leak whether the email exists.
		response.Error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(input.Password)); err != nil {
		response.Error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Login issues a fresh JWT on every successful password check; there is no
	// server-side session table to update or revoke here.
	token, err := GenerateToken(userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not generate token")
		return
	}

	response.Success(w, http.StatusOK, map[string]any{
		"token":   token,
		"user_id": userID,
	})
}
