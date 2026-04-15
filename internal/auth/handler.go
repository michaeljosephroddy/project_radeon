package auth

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project_radeon/api/pkg/response"
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
		FirstName  string  `json:"first_name"`
		LastName   string  `json:"last_name"`
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
	if input.FirstName == "" {
		errs["first_name"] = "required"
	}
	if input.LastName == "" {
		errs["last_name"] = "required"
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

	// Check email not already taken
	var exists bool
	h.db.QueryRow(r.Context(),
		"SELECT EXISTS(SELECT 1 FROM users WHERE email = $1)", input.Email,
	).Scan(&exists)
	if exists {
		response.Error(w, http.StatusConflict, "email already registered")
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
		t, err := time.Parse("2006-01-02", *input.SoberSince)
		if err != nil {
			response.Error(w, http.StatusBadRequest, "sober_since must be YYYY-MM-DD")
			return
		}
		soberSince = &t
	}

	err = h.db.QueryRow(r.Context(),
		`INSERT INTO users (first_name, last_name, email, password_hash, city, country, sober_since)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id`,
		input.FirstName, input.LastName, input.Email, string(hash),
		input.City, input.Country, soberSince,
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
