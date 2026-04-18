package response

import (
	"encoding/json"
	"net/http"
)

type envelope map[string]any

func JSON(w http.ResponseWriter, status int, data any) {
	// All handlers go through this helper so the API's JSON content type and
	// status-writing behavior stay consistent.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func Success(w http.ResponseWriter, status int, data any) {
	JSON(w, status, envelope{"data": data})
}

func Error(w http.ResponseWriter, status int, message string) {
	JSON(w, status, envelope{"error": message})
}

func ValidationError(w http.ResponseWriter, errors map[string]string) {
	JSON(w, http.StatusUnprocessableEntity, envelope{"errors": errors})
}
