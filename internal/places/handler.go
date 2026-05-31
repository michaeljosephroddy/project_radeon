package places

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/project_radeon/api/pkg/response"
)

type Handler struct {
	db Querier
}

func NewHandler(db Querier) *Handler {
	return &Handler{db: db}
}

func (h *Handler) AutocompletePlaces(w http.ResponseWriter, r *http.Request) {
	params, errs := parseAutocompleteParams(r)
	if len(errs) > 0 {
		response.ValidationError(w, errs)
		return
	}
	if len([]rune(params.Query)) < 2 {
		response.Success(w, http.StatusOK, []PlaceSuggestion{})
		return
	}
	suggestions, err := h.db.AutocompletePlaces(r.Context(), params)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not autocomplete places")
		return
	}
	response.Success(w, http.StatusOK, suggestions)
}

func parseAutocompleteParams(r *http.Request) (AutocompleteParams, map[string]string) {
	query := r.URL.Query()
	errs := map[string]string{}
	limit := 8
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			errs["limit"] = "invalid"
		} else if parsed > 10 {
			limit = 10
		} else {
			limit = parsed
		}
	}
	return AutocompleteParams{
		Query:       strings.TrimSpace(query.Get("q")),
		CountryCode: strings.TrimSpace(strings.ToUpper(query.Get("country_code"))),
		Limit:       limit,
	}, errs
}
