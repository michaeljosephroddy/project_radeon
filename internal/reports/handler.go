package reports

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/project_radeon/api/pkg/middleware"
	"github.com/project_radeon/api/pkg/response"
)

type Store interface {
	CreateContentReport(ctx context.Context, input ContentReportInput) error
}

type Handler struct {
	store Store
}

func NewHandler(store Store) *Handler {
	return &Handler{store: store}
}

type ContentReportInput struct {
	ReporterID uuid.UUID
	TargetType string
	TargetID   uuid.UUID
	Reason     string
	Details    *string
	Context    map[string]any
}

type reportRequest struct {
	TargetType string         `json:"target_type"`
	TargetID   string         `json:"target_id"`
	Reason     string         `json:"reason"`
	Details    *string        `json:"details"`
	Context    map[string]any `json:"context"`
}

var validTargetTypes = map[string]bool{
	"feed_post":          true,
	"feed_share":         true,
	"feed_comment":       true,
	"feed_share_comment": true,
	"chat":               true,
	"message":            true,
}

var validReasons = map[string]bool{
	"harassment":     true,
	"spam":           true,
	"safety_concern": true,
	"hate":           true,
	"sexual_content": true,
	"violence":       true,
	"self_harm":      true,
	"other":          true,
}

func (h *Handler) CreateContentReport(w http.ResponseWriter, r *http.Request) {
	reporterID := middleware.CurrentUserID(r)

	var req reportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	targetType := strings.TrimSpace(req.TargetType)
	if !validTargetTypes[targetType] {
		response.ValidationError(w, map[string]string{"target_type": "invalid report target"})
		return
	}

	targetID, err := uuid.Parse(strings.TrimSpace(req.TargetID))
	if err != nil {
		response.ValidationError(w, map[string]string{"target_id": "invalid target id"})
		return
	}

	reason := strings.TrimSpace(req.Reason)
	if !validReasons[reason] {
		response.ValidationError(w, map[string]string{"reason": "invalid report reason"})
		return
	}

	details := trimOptional(req.Details)
	if details != nil && len(*details) > 1000 {
		response.ValidationError(w, map[string]string{"details": "details must be 1000 characters or fewer"})
		return
	}

	if err := h.store.CreateContentReport(r.Context(), ContentReportInput{
		ReporterID: reporterID,
		TargetType: targetType,
		TargetID:   targetID,
		Reason:     reason,
		Details:    details,
		Context:    req.Context,
	}); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not create report")
		return
	}

	response.Success(w, http.StatusCreated, map[string]bool{"reported": true})
}

func trimOptional(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
