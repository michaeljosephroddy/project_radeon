package auth

import "testing"

func TestNormalizeRegisterInput(t *testing.T) {
	input := normalizeRegisterInput(registerInput{
		Username: "  Test.User  ",
		Email:    "  User@Example.COM ",
	})

	if input.Username != "test.user" {
		t.Fatalf("Username = %q, want %q", input.Username, "test.user")
	}
	if input.Email != "user@example.com" {
		t.Fatalf("Email = %q, want %q", input.Email, "user@example.com")
	}
}

func TestValidateRegisterInput(t *testing.T) {
	errs := validateRegisterInput(registerInput{
		Username: "ab",
		Email:    "",
		Password: "short",
	})

	if errs["username"] == "" || errs["email"] == "" || errs["password"] == "" {
		t.Fatalf("expected username/email/password errors, got %+v", errs)
	}
}

func TestValidateRegisterInputIdentityFields(t *testing.T) {
	gender := "robot"
	birthDate := "14-05-1990"
	errs := validateRegisterInput(registerInput{
		Username:  "valid.user",
		Email:     "user@example.com",
		Password:  "password123",
		Gender:    &gender,
		BirthDate: &birthDate,
	})

	if errs["gender"] == "" || errs["birth_date"] == "" {
		t.Fatalf("expected gender and birth_date errors, got %+v", errs)
	}
}

func TestParseSoberSince(t *testing.T) {
	got, err := parseSoberSince(nil)
	if err != nil || got != nil {
		t.Fatalf("parseSoberSince(nil) = %v, %v; want nil, nil", got, err)
	}

	raw := "2026-04-01"
	got, err = parseSoberSince(&raw)
	if err != nil {
		t.Fatalf("parseSoberSince(valid) error = %v", err)
	}
	if got == nil || got.Format("2006-01-02") != raw {
		t.Fatalf("unexpected parsed value: %v", got)
	}

	raw = "04/01/2026"
	if _, err := parseSoberSince(&raw); err == nil {
		t.Fatal("expected invalid sober_since to fail")
	}
}

func TestParseBirthDate(t *testing.T) {
	got, err := parseBirthDate(nil)
	if err != nil || got != nil {
		t.Fatalf("parseBirthDate(nil) = %v, %v; want nil, nil", got, err)
	}

	raw := "1990-05-14"
	got, err = parseBirthDate(&raw)
	if err != nil {
		t.Fatalf("parseBirthDate(valid) error = %v", err)
	}
	if got == nil || got.Format("2006-01-02") != raw {
		t.Fatalf("unexpected parsed value: %v", got)
	}

	raw = "14-05-1990"
	if _, err := parseBirthDate(&raw); err == nil {
		t.Fatal("expected invalid birth_date to fail")
	}
}

func TestNormalizeRegisterGender(t *testing.T) {
	got, ok := normalizeRegisterGender("Women")
	if !ok || got != "woman" {
		t.Fatalf("normalizeRegisterGender(Women) = %q, %v; want woman, true", got, ok)
	}

	got, ok = normalizeRegisterGender("non-binary")
	if !ok || got != "non_binary" {
		t.Fatalf("normalizeRegisterGender(non-binary) = %q, %v; want non_binary, true", got, ok)
	}

	if _, ok := normalizeRegisterGender("robot"); ok {
		t.Fatal("expected invalid gender to fail")
	}
}

func TestNormalizeLoginInput(t *testing.T) {
	input := normalizeLoginInput(loginInput{Email: " User@Example.COM "})
	if input.Email != "user@example.com" {
		t.Fatalf("Email = %q, want %q", input.Email, "user@example.com")
	}
}
