package notifications

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/project_radeon/api/pkg/middleware"
	"github.com/project_radeon/api/pkg/pagination"
	"github.com/project_radeon/api/pkg/response"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterDevice(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	var input RegisterDeviceInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	device, err := h.service.RegisterDevice(r.Context(), userID, input)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(w, http.StatusCreated, device)
}

func (h *Handler) DeleteDevice(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	deviceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid device id")
		return
	}

	if err := h.service.DeleteDevice(r.Context(), userID, deviceID); err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Error(w, http.StatusNotFound, "device not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not delete device")
		return
	}
	response.Success(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (h *Handler) GetPreferences(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	prefs, err := h.service.GetPreferences(r.Context(), userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch preferences")
		return
	}
	response.Success(w, http.StatusOK, prefs)
}

func (h *Handler) UpdatePreferences(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	current, err := h.service.GetPreferences(r.Context(), userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch preferences")
		return
	}

	var input struct {
		ChatMessages    *bool `json:"chat_messages"`
		CommentMentions *bool `json:"comment_mentions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	next := Preferences{
		ChatMessages:    current.ChatMessages,
		CommentMentions: current.CommentMentions,
	}
	if input.ChatMessages != nil {
		next.ChatMessages = *input.ChatMessages
	}
	if input.CommentMentions != nil {
		next.CommentMentions = *input.CommentMentions
	}

	updated, err := h.service.UpdatePreferences(r.Context(), userID, next)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not update preferences")
		return
	}
	response.Success(w, http.StatusOK, updated)
}

func (h *Handler) ListNotifications(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	params := pagination.ParseCursor(r, 20, 50)
	items, err := h.service.ListNotifications(r.Context(), userID, params.Before, params.Limit+1)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch notifications")
		return
	}
	response.Success(w, http.StatusOK, pagination.CursorSlice(items, params.Limit, func(item Notification) time.Time {
		return item.CreatedAt
	}))
}

func (h *Handler) GetSummary(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	summary, err := h.service.GetSummary(r.Context(), userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch notification summary")
		return
	}
	response.Success(w, http.StatusOK, summary)
}

func (h *Handler) MarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	notificationID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid notification id")
		return
	}

	if err := h.service.MarkNotificationRead(r.Context(), userID, notificationID); err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Error(w, http.StatusNotFound, "notification not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not mark notification read")
		return
	}
	response.Success(w, http.StatusOK, map[string]bool{"read": true})
}

func (h *Handler) MarkNotificationsRead(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	var input struct {
		NotificationIDs []uuid.UUID `json:"notification_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	result, err := h.service.MarkNotificationsRead(r.Context(), userID, input.NotificationIDs)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not mark notifications read")
		return
	}
	response.Success(w, http.StatusOK, result)
}

func (h *Handler) MarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	result, err := h.service.MarkAllNotificationsRead(r.Context(), userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not mark notifications read")
		return
	}
	response.Success(w, http.StatusOK, result)
}
