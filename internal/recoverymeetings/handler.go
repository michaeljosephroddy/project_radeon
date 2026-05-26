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

func (h *Handler) ListFilterOptions(w http.ResponseWriter, r *http.Request) {
	params, errs := parseFilterOptionsParams(r)
	if len(errs) > 0 {
		response.ValidationError(w, errs)
		return
	}
	if len([]rune(params.Query)) < 2 {
		response.Success(w, http.StatusOK, []FilterOption{})
		return
	}
	if (params.Level == FilterOptionLevelRegion || params.Level == FilterOptionLevelLocality) && params.Country == "" {
		response.Success(w, http.StatusOK, []FilterOption{})
		return
	}
	options, err := h.db.ListFilterOptions(r.Context(), params)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch recovery meeting filter options")
		return
	}
	response.Success(w, http.StatusOK, options)
}

func (h *Handler) ListLocationSuggestions(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	country := strings.TrimSpace(r.URL.Query().Get("country"))
	if len([]rune(query)) < 2 {
		response.Success(w, http.StatusOK, []LocationSuggestion{})
		return
	}
	if country == "" {
		response.Success(w, http.StatusOK, []LocationSuggestion{})
		return
	}
	suggestions, err := h.db.ListLocationSuggestions(
		r.Context(),
		query,
		country,
		strings.TrimSpace(r.URL.Query().Get("region")),
		strings.TrimSpace(strings.ToLower(r.URL.Query().Get("fellowship"))),
		parseSuggestionLimit(r),
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch recovery meeting locations")
		return
	}
	response.Success(w, http.StatusOK, suggestions)
}

func (h *Handler) ListRegionSuggestions(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	country := strings.TrimSpace(r.URL.Query().Get("country"))
	if len([]rune(query)) < 2 || country == "" {
		response.Success(w, http.StatusOK, []RegionSuggestion{})
		return
	}
	suggestions, err := h.db.ListRegionSuggestions(
		r.Context(),
		query,
		country,
		strings.TrimSpace(strings.ToLower(r.URL.Query().Get("fellowship"))),
		parseSuggestionLimit(r),
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch recovery meeting regions")
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

func parseFilterOptionLimit(r *http.Request) int {
	limit := 10
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

func parseFilterOptionsParams(r *http.Request) (FilterOptionsParams, map[string]string) {
	query := r.URL.Query()
	errs := map[string]string{}
	level := FilterOptionLevel(strings.TrimSpace(strings.ToLower(query.Get("level"))))
	switch level {
	case FilterOptionLevelCountry, FilterOptionLevelRegion, FilterOptionLevelLocality:
	default:
		errs["level"] = "invalid"
	}
	fellowships, fellowshipErr := parseFellowshipFilters(query["fellowship"])
	if fellowshipErr != "" {
		errs["fellowship"] = fellowshipErr
	}
	if len(errs) > 0 {
		return FilterOptionsParams{}, errs
	}
	return FilterOptionsParams{
		Level:       level,
		Query:       strings.TrimSpace(query.Get("q")),
		Fellowships: fellowships,
		Country:     strings.TrimSpace(query.Get("country")),
		Region:      strings.TrimSpace(query.Get("region")),
		Limit:       parseFilterOptionLimit(r),
	}, nil
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
