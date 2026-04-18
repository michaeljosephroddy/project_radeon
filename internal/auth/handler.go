package auth

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project_radeon/api/pkg/response"
	"github.com/project_radeon/api/pkg/username"
	"golang.org/x/crypto/bcrypt"
)

type Handler struct {
	db *pgxpool.Pool
}

func NewHandler(db *pgxpool.Pool) *Handler {
	return &Handler{db: db}
}

// POST /auth/register
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username   string  `json:"username"`
		Email      string  `json:"email"`
		Password   string  `json:"password"`
		City       string  `json:"city"`
		Country    string  `json:"country"`
		SoberSince *string `json:"sober_since"` // nullable — user can skip
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Basic validation
	errs := map[string]string{}
	input.Username = username.Normalize(input.Username)
	if msg := username.ValidationError(input.Username); msg != "" {
		errs["username"] = msg
	}
	if input.Email == "" {
		errs["email"] = "required"
	}
	if len(input.Password) < 8 {
		errs["password"] = "must be at least 8 characters"
	}
	if len(errs) > 0 {
		response.ValidationError(w, errs)
		return
	}

	// Uniqueness checks stay explicit so the handler can return field-specific
	// API errors instead of a generic database constraint failure.
	var exists bool
	if err := h.db.QueryRow(r.Context(),
		"SELECT EXISTS(SELECT 1 FROM users WHERE email = $1)", input.Email,
	).Scan(&exists); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not validate email")
		return
	}
	if exists {
		response.Error(w, http.StatusConflict, "email already registered")
		return
	}

	if err := h.db.QueryRow(r.Context(),
		"SELECT EXISTS(SELECT 1 FROM users WHERE username = $1)", input.Username,
	).Scan(&exists); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not validate username")
		return
	}
	if exists {
		response.Error(w, http.StatusConflict, "username already taken")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not hash password")
		return
	}

	var userID uuid.UUID
	var soberSince *time.Time

	if input.SoberSince != nil {
		// The API accepts a date-only value and stores it as a timestamp to match
		// the nullable schema column used elsewhere in the app.
		t, err := time.Parse("2006-01-02", *input.SoberSince)
		if err != nil {
			response.Error(w, http.StatusBadRequest, "sober_since must be YYYY-MM-DD")
			return
		}
		soberSince = &t
	}

	err = h.db.QueryRow(r.Context(),
		`INSERT INTO users (
			username,
			email,
			password_hash,
			city,
			country,
			sober_since
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`,
		input.Username, input.Email, string(hash), input.City, input.Country, soberSince,
	).Scan(&userID)
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

// POST /auth/login
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var userID uuid.UUID
	var passwordHash string

	err := h.db.QueryRow(r.Context(),
		"SELECT id, password_hash FROM users WHERE email = $1", input.Email,
	).Scan(&userID, &passwordHash)
	if err != nil {
		// Don't leak whether the email exists
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
