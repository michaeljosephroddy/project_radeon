package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/project_radeon/api/internal/recoverymeetings"
	"github.com/project_radeon/api/pkg/database"
)

func main() {
	_ = godotenv.Load()

	var opts recoverymeetings.ImportOptions
	var timeout time.Duration
	flag.StringVar(&opts.SnapshotPath, "snapshot", "", "path to recovery meeting snapshot JSON")
	flag.BoolVar(&opts.DryRun, "dry-run", false, "validate and count import work without committing changes")
	flag.BoolVar(&opts.AllowEmpty, "allow-empty", false, "allow importing an empty snapshot")
	flag.BoolVar(&opts.AllowLargeDrop, "allow-large-drop", false, "allow snapshots more than 20 percent smaller than current active data")
	flag.DurationVar(&timeout, "timeout", 15*time.Minute, "maximum time allowed for the import")
	flag.Parse()

	if opts.SnapshotPath == "" {
		fmt.Fprintln(os.Stderr, "--snapshot is required")
		flag.Usage()
		os.Exit(2)
	}
	if timeout <= 0 {
		fmt.Fprintln(os.Stderr, "--timeout must be positive")
		flag.Usage()
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	pool, err := database.Connect()
	if err != nil {
		log.Fatalf("database connection failed: %v", err)
	}
	defer pool.Close()

	result, err := recoverymeetings.ImportSnapshot(ctx, pool, opts)
	if err != nil {
		log.Fatalf("import recovery meetings failed: %v", err)
	}

	mode := "committed"
	if result.DryRun {
		mode = "dry-run"
	}
	fmt.Printf("Recovery meeting import %s\n", mode)
	if result.ImportRunID != nil {
		fmt.Printf("Import run: %s\n", result.ImportRunID)
	}
	fmt.Printf("Snapshot SHA-256: %s\n", result.SnapshotSHA256)
	fmt.Printf("Meetings seen: %d\n", result.MeetingsSeen)
	fmt.Printf("Meetings upserted: %d\n", result.MeetingsUpserted)
	fmt.Printf("Occurrences written: %d\n", result.OccurrencesWritten)
	fmt.Printf("Marked stale: %d\n", result.StaleMarked)
	fmt.Printf("Marked inactive: %d\n", result.InactiveMarked)
}
