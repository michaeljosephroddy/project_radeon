package dating

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/project_radeon/api/internal/user"
	"github.com/project_radeon/api/pkg/middleware"
	"github.com/project_radeon/api/pkg/pagination"
	"github.com/project_radeon/api/pkg/response"
)

const datingCursorVersion = 1

type datingDiscoverCursor struct {
	Mode      string `json:"mode"`
	Version   int    `json:"version"`
	RequestID string `json:"request_id"`
	Offset    int    `json:"offset"`
}

type Querier interface {
	Discover(ctx context.Context, params DiscoverParams) ([]user.User, error)
	CountDiscover(ctx context.Context, params DiscoverParams) (int, error)
	ListLikes(ctx context.Context, userID uuid.UUID, before *string, limit int) ([]DatingLike, error)
	CountLikes(ctx context.Context, userID uuid.UUID) (int, error)
	RecordAction(ctx context.Context, actorID, targetID uuid.UUID, action string) (*ActionResult, error)
	ListMatches(ctx context.Context, userID uuid.UUID, before *string, limit int) ([]DatingMatch, error)
	GetMatch(ctx context.Context, userID, matchID uuid.UUID) (*DatingMatch, error)
	Unmatch(ctx context.Context, userID, matchID uuid.UUID) (*DatingMatch, error)
}

type Notifier interface {
	NotifyDatingMatch(ctx context.Context, matchID, chatID, actorID, recipientID uuid.UUID) error
}

type Handler struct {
	db       Querier
	notifier Notifier
}

func NewHandler(db Querier, notifier Notifier) *Handler {
	return &Handler{db: db, notifier: notifier}
}

func (h *Handler) Discover(w http.ResponseWriter, r *http.Request) {
	params, err := parseDiscoverRequest(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	params.CurrentUserID = middleware.CurrentUserID(r)

	users, err := h.db.Discover(r.Context(), params)
	if err != nil {
		writeDatingError(w, err)
		return
	}

	limit := params.Limit
	hasMore := len(users) > limit
	if hasMore {
		users = users[:limit]
	}

	var nextCursor *string
	if hasMore {
		next := encodeDatingCursor(params.CursorRequestID, params.CursorOffset+limit)
		nextCursor = &next
	}

	response.Success(w, http.StatusOK, pagination.CursorResponse[user.User]{
		Items:      users,
		Limit:      limit,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	})
}

func (h *Handler) DiscoverPreview(w http.ResponseWriter, r *http.Request) {
	params, err := parseDiscoverRequest(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	params.CurrentUserID = middleware.CurrentUserID(r)

	count, err := h.db.CountDiscover(r.Context(), params)
	if err != nil {
		writeDatingError(w, err)
		return
	}

	response.Success(w, http.StatusOK, PreviewResponse{ExactCount: count})
}

func (h *Handler) ListLikes(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	params := pagination.ParseCursor(r, 12, 50)
	var before *string
	if params.Before != nil {
		value := params.Before.UTC().Format(time.RFC3339Nano)
		before = &value
	}

	likes, err := h.db.ListLikes(r.Context(), userID, before, params.Limit+1)
	if err != nil {
		writeDatingError(w, err)
		return
	}

	users := make([]user.User, 0, len(likes))
	for _, like := range likes {
		users = append(users, like.User)
	}

	hasMore := len(likes) > params.Limit
	if hasMore {
		users = users[:params.Limit]
		likes = likes[:params.Limit]
	}

	var nextCursor *string
	if hasMore && len(likes) > 0 {
		value := likes[len(likes)-1].LikedAt.UTC().Format(time.RFC3339Nano)
		nextCursor = &value
	}

	response.Success(w, http.StatusOK, pagination.CursorResponse[user.User]{
		Items:      users,
		Limit:      params.Limit,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	})
}

func (h *Handler) LikesPreview(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	count, err := h.db.CountLikes(r.Context(), userID)
	if err != nil {
		writeDatingError(w, err)
		return
	}
	response.Success(w, http.StatusOK, PreviewResponse{ExactCount: count})
}

func (h *Handler) RecordAction(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	var input struct {
		TargetUserID string `json:"target_user_id"`
		Action       string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	targetID, err := uuid.Parse(strings.TrimSpace(input.TargetUserID))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "target_user_id must be a valid user id")
		return
	}

	action := strings.TrimSpace(input.Action)
	if action != ActionLike && action != ActionPass {
		response.Error(w, http.StatusBadRequest, "action must be like or pass")
		return
	}

	result, err := h.db.RecordAction(r.Context(), userID, targetID, action)
	if err != nil {
		writeDatingError(w, err)
		return
	}

	if result.Matched && result.Match != nil && result.Match.ChatID != nil && h.notifier != nil {
		_ = h.notifier.NotifyDatingMatch(r.Context(), result.Match.ID, *result.Match.ChatID, userID, targetID)
	}

	status := http.StatusOK
	if action == ActionLike || action == ActionPass {
		status = http.StatusCreated
	}
	response.Success(w, status, result)
}

func (h *Handler) ListMatches(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	params := pagination.ParseCursor(r, 20, 50)
	var before *string
	if params.Before != nil {
		value := params.Before.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
		before = &value
	}

	matches, err := h.db.ListMatches(r.Context(), userID, before, params.Limit+1)
	if err != nil {
		writeDatingError(w, err)
		return
	}

	response.Success(w, http.StatusOK, pagination.CursorSlice(matches, params.Limit, func(match DatingMatch) time.Time {
		return match.MatchedAt
	}))
}

func (h *Handler) GetMatch(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	matchID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid match id")
		return
	}

	match, err := h.db.GetMatch(r.Context(), userID, matchID)
	if err != nil {
		writeDatingError(w, err)
		return
	}
	response.Success(w, http.StatusOK, match)
}

func (h *Handler) Unmatch(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	matchID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid match id")
		return
	}

	match, err := h.db.Unmatch(r.Context(), userID, matchID)
	if err != nil {
		writeDatingError(w, err)
		return
	}
	response.Success(w, http.StatusOK, match)
}

func writeDatingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		response.Error(w, http.StatusNotFound, "dating resource not found")
	case errors.Is(err, ErrDatingDisabled):
		response.Error(w, http.StatusForbidden, "turn on Dating mode to use Dating")
	case errors.Is(err, ErrTargetUnavailable):
		response.Error(w, http.StatusForbidden, "this user is not available in Dating")
	case errors.Is(err, ErrForbidden):
		response.Error(w, http.StatusForbidden, "dating action is not allowed")
	case errors.Is(err, ErrConflict):
		response.Error(w, http.StatusConflict, "dating action already recorded")
	default:
		response.Error(w, http.StatusInternalServerError, "dating request failed")
	}
}

func parseDiscoverRequest(r *http.Request) (DiscoverParams, error) {
	query := r.URL.Query()
	limit := parsePositiveInt(query.Get("limit"), 20)
	if limit > 50 {
		limit = 50
	}

	params := DiscoverParams{
		Gender:    strings.TrimSpace(query.Get("gender")),
		Sobriety:  strings.TrimSpace(query.Get("sobriety")),
		Interests: query["interest"],
		Cursor:    strings.TrimSpace(query.Get("cursor")),
		Limit:     limit,
	}
	decodedCursor := decodeDatingCursor(params.Cursor)
	params.CursorOffset = decodedCursor.Offset
	params.CursorRequestID = decodedCursor.RequestID
	if params.CursorRequestID == "" {
		params.CursorRequestID = uuid.NewString()
	}

	if params.Gender != "" && params.Gender != "woman" && params.Gender != "man" && params.Gender != "non_binary" {
		return DiscoverParams{}, fmt.Errorf("gender must be woman, man, or non_binary")
	}
	if params.Sobriety != "" && params.Sobriety != "days_30" && params.Sobriety != "days_90" && params.Sobriety != "years_1" && params.Sobriety != "years_5" {
		return DiscoverParams{}, fmt.Errorf("sobriety must be 30+ days, 90+ days, 1+ year, or 5+ years")
	}

	var err error
	if params.AgeMin, err = parseOptionalInt(query.Get("age_min"), "age_min"); err != nil {
		return DiscoverParams{}, err
	}
	if params.AgeMax, err = parseOptionalInt(query.Get("age_max"), "age_max"); err != nil {
		return DiscoverParams{}, err
	}
	if params.AgeMin != nil && params.AgeMax != nil && *params.AgeMin > *params.AgeMax {
		return DiscoverParams{}, fmt.Errorf("age_min cannot be greater than age_max")
	}
	if params.DistanceKm, err = parseOptionalInt(query.Get("distance_km"), "distance_km"); err != nil {
		return DiscoverParams{}, err
	}
	if params.Lat, err = parseOptionalFloat(query.Get("lat"), "lat"); err != nil {
		return DiscoverParams{}, err
	}
	if params.Lng, err = parseOptionalFloat(query.Get("lng"), "lng"); err != nil {
		return DiscoverParams{}, err
	}
	if params.DistanceKm != nil && *params.DistanceKm > 0 && (params.Lat == nil || params.Lng == nil) {
		return DiscoverParams{}, fmt.Errorf("lat and lng are required when distance_km is set")
	}
	return params, nil
}

func parsePositiveInt(raw string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

func parseOffset(raw string) int {
	return decodeDatingCursor(raw).Offset
}

func encodeDatingCursor(requestID string, offset int) string {
	if offset < 0 {
		offset = 0
	}
	payload, err := json.Marshal(datingDiscoverCursor{
		Mode:      "dating_ranked",
		Version:   datingCursorVersion,
		RequestID: strings.TrimSpace(requestID),
		Offset:    offset,
	})
	if err != nil {
		return strconv.Itoa(offset)
	}
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeDatingCursor(raw string) datingDiscoverCursor {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return datingDiscoverCursor{}
	}
	if offset, err := strconv.Atoi(raw); err == nil {
		if offset < 0 {
			offset = 0
		}
		return datingDiscoverCursor{Offset: offset}
	}

	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return datingDiscoverCursor{}
	}
	var cursor datingDiscoverCursor
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return datingDiscoverCursor{}
	}
	if cursor.Mode != "dating_ranked" || cursor.Version != datingCursorVersion {
		return datingDiscoverCursor{}
	}
	if cursor.Offset < 0 {
		cursor.Offset = 0
	}
	cursor.RequestID = strings.TrimSpace(cursor.RequestID)
	return cursor
}

func parseOptionalInt(raw string, name string) (*int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return nil, fmt.Errorf("%s must be a valid integer", name)
	}
	return &value, nil
}

func parseOptionalFloat(raw string, name string) (*float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil, fmt.Errorf("%s must be a valid number", name)
	}
	return &value, nil
}
