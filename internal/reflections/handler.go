package reflections

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/project_radeon/api/pkg/middleware"
	"github.com/project_radeon/api/pkg/response"
)

type Handler struct {
	store Store
	now   func() time.Time
}

func NewHandler(store Store) *Handler {
	return &Handler{
		store: store,
		now:   func() time.Time { return time.Now().UTC() },
	}
}

type reflectionRequest struct {
	PromptKey     *string `json:"prompt_key"`
	PromptText    *string `json:"prompt_text"`
	GratefulFor   *string `json:"grateful_for"`
	OnMind        *string `json:"on_mind"`
	BlockingToday *string `json:"blocking_today"`
	Body          *string `json:"body"`
}

func (h *Handler) ListReflections(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	limit := parseLimit(r, 20, 50)
	var before *time.Time
	if raw := strings.TrimSpace(r.URL.Query().Get("before")); raw != "" {
		parsed, err := time.Parse("2006-01-02", raw)
		if err != nil {
			response.Error(w, http.StatusBadRequest, "before must be YYYY-MM-DD")
			return
		}
		before = &parsed
	}

	items, err := h.store.ListReflections(r.Context(), userID, before, limit+1)
	if err != nil {
		log.Printf("list reflections failed for %s: %v", userID, err)
		response.Error(w, http.StatusInternalServerError, "could not fetch reflections")
		return
	}

	response.Success(w, http.StatusOK, buildListResponse(items, limit))
}

func (h *Handler) GetTodayReflection(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	reflection, err := h.store.GetTodayReflection(r.Context(), userID, h.now())
	if err != nil {
		log.Printf("get today reflection failed for %s: %v", userID, err)
		response.Error(w, http.StatusInternalServerError, "could not fetch reflection")
		return
	}
	response.Success(w, http.StatusOK, reflection)
}

func (h *Handler) UpsertTodayReflection(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	input, ok := decodeRequiredReflectionInput(w, r)
	if !ok {
		return
	}

	reflection, err := h.store.UpsertTodayReflection(r.Context(), userID, h.now(), input)
	if err != nil {
		log.Printf("save today reflection failed for %s: %v", userID, err)
		response.Error(w, http.StatusInternalServerError, "could not save reflection")
		return
	}
	response.Success(w, http.StatusOK, reflection)
}

func (h *Handler) GetReflection(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	reflectionID, ok := parseReflectionID(w, r)
	if !ok {
		return
	}

	reflection, err := h.store.GetReflection(r.Context(), userID, reflectionID)
	if errors.Is(err, ErrNotFound) {
		response.Error(w, http.StatusNotFound, "reflection not found")
		return
	}
	if err != nil {
		log.Printf("get reflection failed for %s: %v", userID, err)
		response.Error(w, http.StatusInternalServerError, "could not fetch reflection")
		return
	}
	response.Success(w, http.StatusOK, reflection)
}

func (h *Handler) UpdateReflection(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	reflectionID, ok := parseReflectionID(w, r)
	if !ok {
		return
	}

	input, ok := decodePatchReflectionInput(w, r)
	if !ok {
		return
	}

	reflection, err := h.store.UpdateReflection(r.Context(), userID, reflectionID, input)
	if errors.Is(err, ErrNotFound) {
		response.Error(w, http.StatusNotFound, "reflection not found")
		return
	}
	if err != nil {
		log.Printf("update reflection failed for %s: %v", userID, err)
		response.Error(w, http.StatusInternalServerError, "could not update reflection")
		return
	}
	response.Success(w, http.StatusOK, reflection)
}

func (h *Handler) DeleteReflection(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	reflectionID, ok := parseReflectionID(w, r)
	if !ok {
		return
	}

	err := h.store.DeleteReflection(r.Context(), userID, reflectionID)
	if errors.Is(err, ErrNotFound) {
		response.Error(w, http.StatusNotFound, "reflection not found")
		return
	}
	if err != nil {
		log.Printf("delete reflection failed for %s: %v", userID, err)
		response.Error(w, http.StatusInternalServerError, "could not delete reflection")
		return
	}
	response.Success(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (h *Handler) ShareReflection(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	reflectionID, ok := parseReflectionID(w, r)
	if !ok {
		return
	}

	postID, err := h.store.ShareReflection(r.Context(), userID, reflectionID)
	if errors.Is(err, ErrNotFound) {
		response.Error(w, http.StatusNotFound, "reflection not found")
		return
	}
	if err != nil {
		log.Printf("share reflection failed for %s: %v", userID, err)
		response.Error(w, http.StatusInternalServerError, "could not share reflection")
		return
	}
	response.Success(w, http.StatusOK, map[string]uuid.UUID{"post_id": postID})
}

func decodeRequiredReflectionInput(w http.ResponseWriter, r *http.Request) (UpsertDailyReflectionInput, bool) {
	var req reflectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return UpsertDailyReflectionInput{}, false
	}
	gratefulFor, ok := normalizeSection(req.GratefulFor)
	if !ok {
		response.ValidationError(w, map[string]string{"grateful_for": "keep this answer under 600 characters"})
		return UpsertDailyReflectionInput{}, false
	}
	onMind, ok := normalizeSection(req.OnMind)
	if !ok {
		response.ValidationError(w, map[string]string{"on_mind": "keep this answer under 600 characters"})
		return UpsertDailyReflectionInput{}, false
	}
	blockingToday, ok := normalizeSection(req.BlockingToday)
	if !ok {
		response.ValidationError(w, map[string]string{"blocking_today": "keep this answer under 600 characters"})
		return UpsertDailyReflectionInput{}, false
	}
	body := strings.TrimSpace(valueOrEmpty(req.Body))
	if body == "" {
		body = composeReflectionBody(gratefulFor, onMind, blockingToday)
	}
	if body == "" {
		response.ValidationError(w, map[string]string{"body": "write at least one reflection"})
		return UpsertDailyReflectionInput{}, false
	}
	if len(body) > 2000 {
		response.ValidationError(w, map[string]string{"body": "body must be 2000 characters or fewer"})
		return UpsertDailyReflectionInput{}, false
	}
	return UpsertDailyReflectionInput{
		PromptKey:     trimOptionalString(req.PromptKey),
		PromptText:    trimOptionalString(req.PromptText),
		GratefulFor:   gratefulFor,
		OnMind:        onMind,
		BlockingToday: blockingToday,
		Body:          body,
	}, true
}

func decodePatchReflectionInput(w http.ResponseWriter, r *http.Request) (UpdateDailyReflectionInput, bool) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return UpdateDailyReflectionInput{}, false
	}

	var req reflectionRequest
	encoded, _ := json.Marshal(raw)
	if err := json.Unmarshal(encoded, &req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return UpdateDailyReflectionInput{}, false
	}
	if req.Body != nil {
		body := strings.TrimSpace(*req.Body)
		if body == "" {
			response.ValidationError(w, map[string]string{"body": "body is required"})
			return UpdateDailyReflectionInput{}, false
		}
		if len(body) > 2000 {
			response.ValidationError(w, map[string]string{"body": "body must be 2000 characters or fewer"})
			return UpdateDailyReflectionInput{}, false
		}
		req.Body = &body
	}
	gratefulFor, ok := normalizeSection(req.GratefulFor)
	if !ok {
		response.ValidationError(w, map[string]string{"grateful_for": "keep this answer under 600 characters"})
		return UpdateDailyReflectionInput{}, false
	}
	onMind, ok := normalizeSection(req.OnMind)
	if !ok {
		response.ValidationError(w, map[string]string{"on_mind": "keep this answer under 600 characters"})
		return UpdateDailyReflectionInput{}, false
	}
	blockingToday, ok := normalizeSection(req.BlockingToday)
	if !ok {
		response.ValidationError(w, map[string]string{"blocking_today": "keep this answer under 600 characters"})
		return UpdateDailyReflectionInput{}, false
	}
	var input UpdateDailyReflectionInput
	if _, exists := raw["prompt_key"]; exists {
		next := trimOptionalString(req.PromptKey)
		input.PromptKey = &next
	}
	if _, exists := raw["prompt_text"]; exists {
		next := trimOptionalString(req.PromptText)
		input.PromptText = &next
	}
	if _, exists := raw["grateful_for"]; exists {
		input.GratefulFor = &gratefulFor
	}
	if _, exists := raw["on_mind"]; exists {
		input.OnMind = &onMind
	}
	if _, exists := raw["blocking_today"]; exists {
		input.BlockingToday = &blockingToday
	}
	if _, exists := raw["body"]; exists {
		input.Body = req.Body
	}
	return input, true
}

func parseReflectionID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid reflection id")
		return uuid.Nil, false
	}
	return id, true
}

func parseLimit(r *http.Request, defaultLimit, maxLimit int) int {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	return limit
}

func buildListResponse(items []DailyReflection, limit int) ListResponse {
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}

	var nextCursor *string
	if hasMore && len(items) > 0 {
		cursor := items[len(items)-1].ReflectionDate
		nextCursor = &cursor
	}

	return ListResponse{
		Items:      items,
		Limit:      limit,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}
}

func trimOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizeSection(value *string) (*string, bool) {
	if value == nil {
		return nil, true
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil, true
	}
	if len(trimmed) > 600 {
		return nil, false
	}
	return &trimmed, true
}

func composeReflectionBody(gratefulFor, onMind, blockingToday *string) string {
	parts := make([]string, 0, 3)
	if gratefulFor != nil {
		parts = append(parts, "Today I'm grateful for\n"+*gratefulFor)
	}
	if onMind != nil {
		parts = append(parts, "What's on my mind\n"+*onMind)
	}
	if blockingToday != nil {
		parts = append(parts, "What's blocking me today\n"+*blockingToday)
	}
	return strings.Join(parts, "\n\n")
}
