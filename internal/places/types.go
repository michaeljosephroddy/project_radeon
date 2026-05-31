package places

import (
	"time"

	"github.com/google/uuid"
)

type Place struct {
	ID             uuid.UUID `json:"id"`
	Source         string    `json:"source"`
	SourceID       string    `json:"source_id"`
	Name           string    `json:"name"`
	ASCIIName      *string   `json:"ascii_name,omitempty"`
	AlternateNames []string  `json:"alternate_names"`
	CountryCode    string    `json:"country_code"`
	CountryName    *string   `json:"country_name,omitempty"`
	Admin1Code     *string   `json:"admin1_code,omitempty"`
	Admin1Name     *string   `json:"admin1_name,omitempty"`
	Admin2Code     *string   `json:"admin2_code,omitempty"`
	Admin2Name     *string   `json:"admin2_name,omitempty"`
	FeatureClass   *string   `json:"feature_class,omitempty"`
	FeatureCode    *string   `json:"feature_code,omitempty"`
	Population     int       `json:"population"`
	Latitude       float64   `json:"latitude"`
	Longitude      float64   `json:"longitude"`
	Timezone       *string   `json:"timezone,omitempty"`
	SearchText     string    `json:"search_text"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type PlaceSuggestion struct {
	ID          uuid.UUID `json:"id"`
	Label       string    `json:"label"`
	Name        string    `json:"name"`
	Country     string    `json:"country"`
	CountryCode string    `json:"country_code"`
	Region      *string   `json:"region,omitempty"`
	RegionCode  *string   `json:"region_code,omitempty"`
	Latitude    float64   `json:"latitude"`
	Longitude   float64   `json:"longitude"`
	Population  int       `json:"population"`
	Source      string    `json:"source"`
}

type AutocompleteParams struct {
	Query       string
	CountryCode string
	Limit       int
}

type ImportOptions struct {
	GeoNamesCitiesPath string
	CountryInfoPath    string
	Admin1Path         string
	Admin2Path         string
	DryRun             bool
}

type ImportResult struct {
	RowsRead  int  `json:"rows_read"`
	RowsValid int  `json:"rows_valid"`
	RowsSaved int  `json:"rows_saved"`
	DryRun    bool `json:"dry_run"`
}

type MatchRefreshResult struct {
	MeetingsScanned int `json:"meetings_scanned"`
	MatchesWritten  int `json:"matches_written"`
}
