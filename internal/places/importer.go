package places

import (
	"bufio"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const geoNamesSource = "geonames"

type geonamesCity struct {
	SourceID       string
	Name           string
	ASCIIName      string
	AlternateNames []string
	Latitude       float64
	Longitude      float64
	FeatureClass   string
	FeatureCode    string
	CountryCode    string
	Admin1Code     string
	Admin2Code     string
	Population     int
	Timezone       string
}

func ImportGeoNames(ctx context.Context, pool *pgxpool.Pool, opts ImportOptions) (*ImportResult, error) {
	if strings.TrimSpace(opts.GeoNamesCitiesPath) == "" {
		return nil, errors.New("geonames cities path is required")
	}
	countryNames, err := loadCountryNames(opts.CountryInfoPath)
	if err != nil {
		return nil, err
	}
	admin1Names, err := loadAdminNames(opts.Admin1Path)
	if err != nil {
		return nil, err
	}
	admin2Names, err := loadAdminNames(opts.Admin2Path)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(opts.GeoNamesCitiesPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(bufio.NewReader(file))
	reader.Comma = '\t'
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	reader.ReuseRecord = true

	result := &ImportResult{DryRun: opts.DryRun}
	batch := &pgx.Batch{}
	batchSize := 0
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		result.RowsRead++
		city, err := parseGeoNamesCity(row)
		if err != nil {
			continue
		}
		result.RowsValid++
		if opts.DryRun {
			continue
		}
		queuePlaceUpsert(batch, city, countryNames, admin1Names, admin2Names)
		batchSize++
		if batchSize >= 1000 {
			saved, err := sendPlaceBatch(ctx, pool, batch)
			if err != nil {
				return nil, err
			}
			result.RowsSaved += saved
			batch = &pgx.Batch{}
			batchSize = 0
		}
	}
	if !opts.DryRun && batchSize > 0 {
		saved, err := sendPlaceBatch(ctx, pool, batch)
		if err != nil {
			return nil, err
		}
		result.RowsSaved += saved
	}
	if opts.DryRun {
		result.RowsSaved = result.RowsValid
	}
	return result, nil
}

func parseGeoNamesCity(row []string) (geonamesCity, error) {
	if len(row) < 18 {
		return geonamesCity{}, errors.New("invalid geonames row")
	}
	latitude, err := strconv.ParseFloat(row[4], 64)
	if err != nil {
		return geonamesCity{}, err
	}
	longitude, err := strconv.ParseFloat(row[5], 64)
	if err != nil {
		return geonamesCity{}, err
	}
	population := 0
	if raw := strings.TrimSpace(row[14]); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err == nil && parsed > 0 {
			population = parsed
		}
	}
	return geonamesCity{
		SourceID:       strings.TrimSpace(row[0]),
		Name:           strings.TrimSpace(row[1]),
		ASCIIName:      strings.TrimSpace(row[2]),
		AlternateNames: parseAlternateNames(row[3]),
		Latitude:       latitude,
		Longitude:      longitude,
		FeatureClass:   strings.TrimSpace(row[6]),
		FeatureCode:    strings.TrimSpace(row[7]),
		CountryCode:    strings.TrimSpace(strings.ToUpper(row[8])),
		Admin1Code:     strings.TrimSpace(row[10]),
		Admin2Code:     strings.TrimSpace(row[11]),
		Population:     population,
		Timezone:       strings.TrimSpace(row[17]),
	}, nil
}

func parseAlternateNames(raw string) []string {
	parts := strings.Split(raw, ",")
	names := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		names = append(names, name)
	}
	return names
}

func queuePlaceUpsert(batch *pgx.Batch, city geonamesCity, countryNames, admin1Names, admin2Names map[string]string) {
	countryName := countryNames[city.CountryCode]
	admin1Key := city.CountryCode + "." + city.Admin1Code
	admin2Key := city.CountryCode + "." + city.Admin1Code + "." + city.Admin2Code
	admin1Name := admin1Names[admin1Key]
	admin2Name := admin2Names[admin2Key]
	searchText := buildSearchText(
		city.Name,
		city.ASCIIName,
		strings.Join(city.AlternateNames, " "),
		countryName,
		city.CountryCode,
		admin1Name,
		city.Admin1Code,
		admin2Name,
		city.Admin2Code,
		city.SourceID,
	)
	batch.Queue(`
		INSERT INTO places (
			source,
			source_id,
			name,
			ascii_name,
			alternate_names,
			country_code,
			country_name,
			admin1_code,
			admin1_name,
			admin2_code,
			admin2_name,
			feature_class,
			feature_code,
			population,
			latitude,
			longitude,
			timezone,
			search_text,
			updated_at
		)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, ''), NULLIF($10, ''), NULLIF($11, ''), NULLIF($12, ''), NULLIF($13, ''), $14, $15, $16, NULLIF($17, ''), $18, NOW())
		ON CONFLICT (source, source_id) DO UPDATE SET
			name = EXCLUDED.name,
			ascii_name = EXCLUDED.ascii_name,
			alternate_names = EXCLUDED.alternate_names,
			country_code = EXCLUDED.country_code,
			country_name = EXCLUDED.country_name,
			admin1_code = EXCLUDED.admin1_code,
			admin1_name = EXCLUDED.admin1_name,
			admin2_code = EXCLUDED.admin2_code,
			admin2_name = EXCLUDED.admin2_name,
			feature_class = EXCLUDED.feature_class,
			feature_code = EXCLUDED.feature_code,
			population = EXCLUDED.population,
			latitude = EXCLUDED.latitude,
			longitude = EXCLUDED.longitude,
			timezone = EXCLUDED.timezone,
			search_text = EXCLUDED.search_text,
			updated_at = NOW()
	`, geoNamesSource, city.SourceID, city.Name, city.ASCIIName, city.AlternateNames, city.CountryCode, countryName, city.Admin1Code, admin1Name, city.Admin2Code, admin2Name, city.FeatureClass, city.FeatureCode, city.Population, city.Latitude, city.Longitude, city.Timezone, searchText)
}

func sendPlaceBatch(ctx context.Context, pool *pgxpool.Pool, batch *pgx.Batch) (int, error) {
	results := pool.SendBatch(ctx, batch)
	defer results.Close()
	count := batch.Len()
	for i := 0; i < count; i++ {
		if _, err := results.Exec(); err != nil {
			return 0, err
		}
	}
	return count, nil
}

func loadCountryNames(path string) (map[string]string, error) {
	names := map[string]string{}
	if strings.TrimSpace(path) == "" {
		return names, nil
	}
	return loadDelimitedLookup(path, func(row []string) (string, string, bool) {
		if len(row) < 5 || strings.HasPrefix(row[0], "#") {
			return "", "", false
		}
		return strings.TrimSpace(strings.ToUpper(row[0])), strings.TrimSpace(row[4]), true
	})
}

func loadAdminNames(path string) (map[string]string, error) {
	names := map[string]string{}
	if strings.TrimSpace(path) == "" {
		return names, nil
	}
	return loadDelimitedLookup(path, func(row []string) (string, string, bool) {
		if len(row) < 2 || strings.HasPrefix(row[0], "#") {
			return "", "", false
		}
		return strings.TrimSpace(row[0]), strings.TrimSpace(row[1]), true
	})
}

func loadDelimitedLookup(path string, pick func([]string) (string, string, bool)) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := csv.NewReader(bufio.NewReader(file))
	reader.Comma = '\t'
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	values := map[string]string{}
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		key, value, ok := pick(row)
		if ok && key != "" && value != "" {
			values[key] = value
		}
	}
	return values, nil
}

func buildSearchText(parts ...string) string {
	values := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		values = append(values, trimmed)
	}
	return strings.Join(values, " ")
}

func FormatImportResult(result *ImportResult) string {
	if result == nil {
		return ""
	}
	mode := "import"
	if result.DryRun {
		mode = "dry run"
	}
	return fmt.Sprintf("%s complete: rows_read=%d rows_valid=%d rows_saved=%d", mode, result.RowsRead, result.RowsValid, result.RowsSaved)
}
