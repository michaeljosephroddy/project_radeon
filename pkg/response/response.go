package response

import (
	"encoding/json"
	"net/http"
)

type envelope map[string]any

// JSON writes a JSON response body with the supplied HTTP status code.
func JSON(w http.ResponseWriter, status int, data any) {
	// All handlers go through this helper so the API's JSON content type and
	// status-writing behavior stay consistent.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// Success wraps a successful payload in the API's standard data envelope.
func Success(w http.ResponseWriter, status int, data any) {
	JSON(w, status, envelope{"data": data})
}

// Error wraps an error message in the API's standard error envelope.
func Error(w http.ResponseWriter, status int, message string) {
	JSON(w, status, envelope{"error": message})
}

// ValidationError writes field-level validation failures with a 422 status.
func ValidationError(w http.ResponseWriter, errors map[string]string) {
	JSON(w, http.StatusUnprocessableEntity, envelope{"errors": errors})
}
