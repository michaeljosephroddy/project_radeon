package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type mockQuerier struct {
	emailExists    func(ctx context.Context, email string) (bool, error)
	usernameExists func(ctx context.Context, username string) (bool, error)
	createUser     func(ctx context.Context, username, email, passwordHash, city, country string, gender *string, birthDate, soberSince *time.Time) (uuid.UUID, error)
	getUserCreds   func(ctx context.Context, email string) (uuid.UUID, string, error)
}

func (m *mockQuerier) EmailExists(ctx context.Context, email string) (bool, error) {
	if m.emailExists != nil {
		return m.emailExists(ctx, email)
	}
	return false, nil
}
func (m *mockQuerier) UsernameExists(ctx context.Context, username string) (bool, error) {
	if m.usernameExists != nil {
		return m.usernameExists(ctx, username)
	}
	return false, nil
}
func (m *mockQuerier) CreateUser(ctx context.Context, username, email, passwordHash, city, country string, gender *string, birthDate, soberSince *time.Time) (uuid.UUID, error) {
	if m.createUser != nil {
		return m.createUser(ctx, username, email, passwordHash, city, country, gender, birthDate, soberSince)
	}
	return uuid.New(), nil
}
func (m *mockQuerier) GetUserCredentials(ctx context.Context, email string) (uuid.UUID, string, error) {
	if m.getUserCreds != nil {
		return m.getUserCreds(ctx, email)
	}
	return uuid.Nil, "", errors.New("not found")
}

const validRegisterBody = `{"username":"valid.user","email":"user@example.com","password":"password123"}`

// ── Register ──────────────────────────────────────────────────────────────────

func TestRegisterValidationFails(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(`{"username":"ab","email":"","password":"short"}`))
	rec := httptest.NewRecorder()

	h.Register(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	assertErrorFields(t, rec, "username", "email", "password")
}

func TestRegisterInvalidSoberSince(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(`{"username":"valid.user","email":"user@example.com","password":"password123","sober_since":"04/19/2026"}`))
	rec := httptest.NewRecorder()

	h.Register(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestRegisterInvalidBirthDate(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(`{"username":"valid.user","email":"user@example.com","password":"password123","birth_date":"04/19/1990"}`))
	rec := httptest.NewRecorder()

	h.Register(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	assertErrorFields(t, rec, "birth_date")
}

func TestRegisterInvalidGender(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(`{"username":"valid.user","email":"user@example.com","password":"password123","gender":"robot"}`))
	rec := httptest.NewRecorder()

	h.Register(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	assertErrorFields(t, rec, "gender")
}

func TestRegisterEmailConflict(t *testing.T) {
	h := NewHandler(&mockQuerier{
		emailExists: func(_ context.Context, _ string) (bool, error) { return true, nil },
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(validRegisterBody))
	rec := httptest.NewRecorder()

	h.Register(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestRegisterUsernameConflict(t *testing.T) {
	h := NewHandler(&mockQuerier{
		usernameExists: func(_ context.Context, _ string) (bool, error) { return true, nil },
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(validRegisterBody))
	rec := httptest.NewRecorder()

	h.Register(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestRegisterDBErrorOnCreate(t *testing.T) {
	h := NewHandler(&mockQuerier{
		createUser: func(_ context.Context, _, _, _, _, _ string, _ *string, _, _ *time.Time) (uuid.UUID, error) {
			return uuid.Nil, errors.New("db error")
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(validRegisterBody))
	rec := httptest.NewRecorder()

	h.Register(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestRegisterSuccess(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")
	fixed := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	var gotGender *string
	var gotBirthDate *time.Time
	h := NewHandler(&mockQuerier{
		createUser: func(_ context.Context, _, _, _, _, _ string, gender *string, birthDate, _ *time.Time) (uuid.UUID, error) {
			gotGender = gender
			gotBirthDate = birthDate
			return fixed, nil
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(`{"username":"valid.user","email":"user@example.com","password":"password123","gender":"Women","birth_date":"1990-05-14"}`))
	rec := httptest.NewRecorder()

	h.Register(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	var body struct {
		Data struct {
			Token  string    `json:"token"`
			UserID uuid.UUID `json:"user_id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if body.Data.Token == "" {
		t.Fatal("expected a non-empty token")
	}
	if body.Data.UserID != fixed {
		t.Fatalf("user_id = %v, want %v", body.Data.UserID, fixed)
	}
	if gotGender == nil || *gotGender != "woman" {
		t.Fatalf("gender = %v, want woman", gotGender)
	}
	if gotBirthDate == nil || gotBirthDate.Format("2006-01-02") != "1990-05-14" {
		t.Fatalf("birthDate = %v, want 1990-05-14", gotBirthDate)
	}
}

// ── Login ─────────────────────────────────────────────────────────────────────

func TestLoginInvalidBody(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader("{"))
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestLoginUserNotFound(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getUserCreds: func(_ context.Context, _ string) (uuid.UUID, string, error) {
			return uuid.Nil, "", errors.New("not found")
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"email":"user@example.com","password":"password123"}`))
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")
	hash, _ := bcrypt.GenerateFromPassword([]byte("correctpassword"), bcrypt.MinCost)
	h := NewHandler(&mockQuerier{
		getUserCreds: func(_ context.Context, _ string) (uuid.UUID, string, error) {
			return uuid.New(), string(hash), nil
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"email":"user@example.com","password":"wrongpassword"}`))
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestLoginSuccess(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")
	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	fixed := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	h := NewHandler(&mockQuerier{
		getUserCreds: func(_ context.Context, _ string) (uuid.UUID, string, error) {
			return fixed, string(hash), nil
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"email":"user@example.com","password":"password123"}`))
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body struct {
		Data struct {
			Token  string    `json:"token"`
			UserID uuid.UUID `json:"user_id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if body.Data.Token == "" {
		t.Fatal("expected a non-empty token")
	}
	if body.Data.UserID != fixed {
		t.Fatalf("user_id = %v, want %v", body.Data.UserID, fixed)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func assertErrorFields(t *testing.T, rec *httptest.ResponseRecorder, fields ...string) {
	t.Helper()
	var body struct {
		Errors map[string]string `json:"errors"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	for _, f := range fields {
		if body.Errors[f] == "" {
			t.Fatalf("expected error for field %q, got %+v", f, body.Errors)
		}
	}
}
