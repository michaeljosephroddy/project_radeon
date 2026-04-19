package username

import "testing"

func TestNormalize(t *testing.T) {
	if got := Normalize("  Alice.Example_1 "); got != "alice.example_1" {
		t.Fatalf("Normalize() = %q, want %q", got, "alice.example_1")
	}
}

func TestValidationError(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "required", value: "", want: "required"},
		{name: "too short", value: "ab", want: "must be between 3 and 20 characters"},
		{name: "invalid characters", value: "Bad-Name", want: "may only contain lowercase letters, numbers, periods, and underscores"},
		{name: "reserved", value: "support", want: "is reserved"},
		{name: "valid", value: "good.name_1", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidationError(tt.value); got != tt.want {
				t.Fatalf("ValidationError(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestIsReserved(t *testing.T) {
	if !IsReserved("admin") {
		t.Fatal("expected admin to be reserved")
	}
	if IsReserved("ordinary_user") {
		t.Fatal("expected ordinary_user to not be reserved")
	}
}
