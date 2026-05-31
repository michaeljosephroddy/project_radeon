package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/project_radeon/api/internal/places"
	"github.com/project_radeon/api/internal/recoverymeetings"
	"github.com/project_radeon/api/pkg/cache"
	"github.com/project_radeon/api/pkg/database"
)

func main() {
	godotenv.Load()

	var opts places.ImportOptions
	var timeout time.Duration
	var refreshRecoveryMeetingMatches bool
	flag.StringVar(&opts.GeoNamesCitiesPath, "geonames-cities", "", "path to GeoNames cities text file, such as cities1000.txt")
	flag.StringVar(&opts.CountryInfoPath, "country-info", "", "path to GeoNames countryInfo.txt")
	flag.StringVar(&opts.Admin1Path, "admin1", "", "path to GeoNames admin1CodesASCII.txt")
	flag.StringVar(&opts.Admin2Path, "admin2", "", "path to GeoNames admin2Codes.txt")
	flag.BoolVar(&opts.DryRun, "dry-run", false, "parse input files without writing places")
	flag.BoolVar(&refreshRecoveryMeetingMatches, "refresh-recovery-meeting-matches", false, "refresh canonical place matches for active recovery meetings")
	flag.DurationVar(&timeout, "timeout", 30*time.Minute, "maximum time allowed for the import")
	flag.Parse()

	if opts.GeoNamesCitiesPath == "" && !refreshRecoveryMeetingMatches {
		flag.Usage()
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if opts.DryRun && opts.GeoNamesCitiesPath != "" && !refreshRecoveryMeetingMatches {
		result, err := places.ImportGeoNames(ctx, nil, opts)
		if err != nil {
			log.Fatalf("import places: %v", err)
		}
		fmt.Println(places.FormatImportResult(result))
		return
	}

	pool, err := database.Connect()
	if err != nil {
		log.Fatalf("database connection failed: %v", err)
	}
	defer pool.Close()

	if opts.GeoNamesCitiesPath != "" {
		result, err := places.ImportGeoNames(ctx, pool, opts)
		if err != nil {
			log.Fatalf("import places: %v", err)
		}
		if !result.DryRun {
			cacheStore, err := newImportCache(ctx)
			if err != nil {
				log.Fatalf("cache initialization failed after places import: %v", err)
			}
			if err := places.BumpCacheVersion(ctx, cacheStore); err != nil {
				log.Fatalf("bump places cache version failed after import: %v", err)
			}
		}
		fmt.Println(places.FormatImportResult(result))
	}

	if refreshRecoveryMeetingMatches {
		result, err := places.RefreshRecoveryMeetingPlaceMatches(ctx, pool)
		if err != nil {
			log.Fatalf("refresh recovery meeting place matches: %v", err)
		}
		cacheStore, err := newImportCache(ctx)
		if err != nil {
			log.Fatalf("cache initialization failed after recovery meeting place match refresh: %v", err)
		}
		if err := recoverymeetings.BumpCacheVersion(ctx, cacheStore); err != nil {
			log.Fatalf("bump recovery meetings cache version failed after place match refresh: %v", err)
		}
		fmt.Printf("recovery meeting place match refresh complete: meetings_scanned=%d matches_written=%d\n", result.MeetingsScanned, result.MatchesWritten)
	}
}

func newImportCache(ctx context.Context) (cache.Store, error) {
	return cache.New(ctx, cache.Config{
		Enabled:  parseBoolEnv("CACHE_ENABLED"),
		Addr:     strings.TrimSpace(os.Getenv("REDIS_ADDR")),
		Password: os.Getenv("REDIS_PASSWORD"),
		DB:       parseIntEnv("REDIS_DB"),
		TLS:      parseBoolEnv("REDIS_TLS"),
		Prefix:   strings.TrimSpace(os.Getenv("REDIS_PREFIX")),
	})
}

func parseBoolEnv(key string) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func parseIntEnv(key string) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return 0
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return parsed
}
