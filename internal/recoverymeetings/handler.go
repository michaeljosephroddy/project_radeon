package recoverymeetings

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/project_radeon/api/pkg/response"
)

type Handler struct {
	db Querier
}

func NewHandler(db Querier) *Handler {
	return &Handler{db: db}
}

func (h *Handler) ListRecoveryMeetings(w http.ResponseWriter, r *http.Request) {
	params, errs := parseListParams(r)
	if len(errs) > 0 {
		response.ValidationError(w, errs)
		return
	}

	meetings, err := h.db.ListRecoveryMeetings(r.Context(), params)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch recovery meetings")
		return
	}
	response.Success(w, http.StatusOK, meetings)
}

func (h *Handler) ListLocationSuggestions(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(query)) < 2 {
		response.Success(w, http.StatusOK, []LocationSuggestion{})
		return
	}
	suggestions, err := h.db.ListLocationSuggestions(
		r.Context(),
		query,
		strings.TrimSpace(r.URL.Query().Get("country")),
		strings.TrimSpace(strings.ToLower(r.URL.Query().Get("fellowship"))),
		parseSuggestionLimit(r),
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch recovery meeting locations")
		return
	}
	response.Success(w, http.StatusOK, suggestions)
}

func (h *Handler) ListCountrySuggestions(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(query)) < 2 {
		response.Success(w, http.StatusOK, []CountrySuggestion{})
		return
	}
	suggestions, err := h.db.ListCountrySuggestions(
		r.Context(),
		query,
		strings.TrimSpace(strings.ToLower(r.URL.Query().Get("fellowship"))),
		parseSuggestionLimit(r),
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch recovery meeting countries")
		return
	}
	response.Success(w, http.StatusOK, suggestions)
}

func (h *Handler) GetRecoveryMeeting(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid recovery meeting id")
		return
	}

	meeting, err := h.db.GetRecoveryMeeting(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Error(w, http.StatusNotFound, "recovery meeting not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not fetch recovery meeting")
		return
	}
	response.Success(w, http.StatusOK, meeting)
}

func parseSuggestionLimit(r *http.Request) int {
	limit := 8
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > 15 {
		return 15
	}
	return limit
}

func parseListParams(r *http.Request) (ListParams, map[string]string) {
	query := r.URL.Query()
	errs := map[string]string{}

	limit := 20
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			errs["limit"] = "invalid"
		} else if parsed > 50 {
			limit = 50
		} else {
			limit = parsed
		}
	}

	var dayOfWeek *int
	if raw := strings.TrimSpace(query.Get("day_of_week")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 || parsed > 6 {
			errs["day_of_week"] = "must be between 0 and 6"
		} else {
			dayOfWeek = &parsed
		}
	}

	meetingType := strings.TrimSpace(strings.ToLower(query.Get("meeting_type")))
	if meetingType == "" {
		meetingType = strings.TrimSpace(strings.ToLower(query.Get("type")))
	}
	if meetingType != "" {
		if _, ok := validMeetingTypes[meetingType]; !ok {
			errs["meeting_type"] = "invalid"
		}
	}

	search := strings.TrimSpace(query.Get("q"))
	if search == "" {
		search = strings.TrimSpace(query.Get("query"))
	}
	location := strings.TrimSpace(query.Get("location"))
	city := strings.TrimSpace(query.Get("city"))
	if location == "" {
		location = city
	}

	if len(errs) > 0 {
		return ListParams{}, errs
	}

	return ListParams{
		Query:       search,
		Fellowship:  strings.TrimSpace(strings.ToLower(query.Get("fellowship"))),
		Country:     strings.TrimSpace(query.Get("country")),
		City:        city,
		Location:    location,
		MeetingType: meetingType,
		DayOfWeek:   dayOfWeek,
		Cursor:      strings.TrimSpace(query.Get("cursor")),
		Limit:       limit,
	}, nil
}
