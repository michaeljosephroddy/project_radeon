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

var validFellowships = map[string]struct{}{
	"aa": {},
	"ca": {},
	"na": {},
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
	fellowships, fellowshipErr := parseFellowshipFilters(query["fellowship"])
	if fellowshipErr != "" {
		errs["fellowship"] = fellowshipErr
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

	var placeID *uuid.UUID
	if raw := strings.TrimSpace(query.Get("place_id")); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			errs["place_id"] = "invalid"
		} else {
			placeID = &parsed
		}
	}

	if len(errs) > 0 {
		return ListParams{}, errs
	}

	return ListParams{
		Query:       search,
		Fellowships: fellowships,
		Country:     strings.TrimSpace(query.Get("country")),
		Region:      strings.TrimSpace(query.Get("region")),
		City:        city,
		Location:    location,
		PlaceID:     placeID,
		MeetingType: meetingType,
		DayOfWeek:   dayOfWeek,
		Cursor:      strings.TrimSpace(query.Get("cursor")),
		Limit:       limit,
	}, nil
}

func parseFellowshipFilters(rawValues []string) ([]string, string) {
	fellowships := []string{}
	seen := map[string]struct{}{}
	for _, rawValue := range rawValues {
		for _, part := range strings.Split(rawValue, ",") {
			fellowship := strings.TrimSpace(strings.ToLower(part))
			if fellowship == "" {
				continue
			}
			if _, ok := validFellowships[fellowship]; !ok {
				return nil, "invalid"
			}
			if _, ok := seen[fellowship]; ok {
				continue
			}
			seen[fellowship] = struct{}{}
			fellowships = append(fellowships, fellowship)
		}
	}
	return fellowships, ""
}
