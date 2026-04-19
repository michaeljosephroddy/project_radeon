package auth

import (
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestGenerateAndParseToken(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")
	t.Setenv("JWT_EXPIRY_HOURS", "2")

	userID := uuid.New()
	token, err := GenerateToken(userID)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	claims, err := ParseToken(token)
	if err != nil {
		t.Fatalf("ParseToken() error = %v", err)
	}
	if claims.UserID != userID {
		t.Fatalf("claims.UserID = %v, want %v", claims.UserID, userID)
	}
}

func TestParseTokenRejectsWrongSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", "first-secret")
	token, err := GenerateToken(uuid.New())
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	t.Setenv("JWT_SECRET", "second-secret")
	if _, err := ParseToken(token); err == nil {
		t.Fatal("expected ParseToken to fail with wrong secret")
	}
}

func TestParseTokenRejectsUnexpectedSigningMethod(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")

	token := jwt.NewWithClaims(jwt.SigningMethodNone, Claims{UserID: uuid.New()})
	raw, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("SignedString() error = %v", err)
	}

	if _, err := ParseToken(raw); err == nil {
		t.Fatal("expected ParseToken to reject non-HMAC token")
	}
}
