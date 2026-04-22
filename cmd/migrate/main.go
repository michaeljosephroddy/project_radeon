package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/project_radeon/api/pkg/database"
)

const (
	migrationsDir = "migrations"
	baseSchema    = "schema/base.sql"
)

type migrationFile struct {
	Name     string
	Path     string
	Checksum string
	SQL      string
}

func main() {
	_ = godotenv.Load()

	command := "up"
	if len(os.Args) > 1 {
		command = strings.TrimSpace(os.Args[1])
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool, err := database.Connect()
	if err != nil {
		fatalf("database connection failed: %v", err)
	}
	defer pool.Close()

	migrations, err := loadMigrations()
	if err != nil {
		fatalf("load migrations: %v", err)
	}

	switch command {
	case "up":
		if err := migrateUp(ctx, pool, migrations); err != nil {
			fatalf("migrate up: %v", err)
		}
	case "status":
		if err := printStatus(ctx, pool, migrations); err != nil {
			fatalf("migrate status: %v", err)
		}
	default:
		fatalf("unknown command %q; expected 'up' or 'status'", command)
	}
}

func migrateUp(ctx context.Context, pool *pgxpool.Pool, migrations []migrationFile) error {
	if err := ensureMigrationTable(ctx, pool); err != nil {
		return err
	}

	applied, err := listApplied(ctx, pool)
	if err != nil {
		return err
	}

	if len(applied) == 0 {
		initialized, err := databaseLooksInitialized(ctx, pool)
		if err != nil {
			return err
		}
		if initialized {
			if err := stampApplied(ctx, pool, migrations); err != nil {
				return fmt.Errorf("baseline existing database: %w", err)
			}
			fmt.Println("Baselined existing database state into schema_migrations")
		} else {
			if err := applyBaseSchema(ctx, pool); err != nil {
				return fmt.Errorf("apply base schema: %w", err)
			}
			if err := stampApplied(ctx, pool, migrations); err != nil {
				return fmt.Errorf("stamp base schema baseline: %w", err)
			}
			fmt.Println("Applied schema/base.sql and initialized schema_migrations baseline")
		}
		applied, err = listApplied(ctx, pool)
		if err != nil {
			return err
		}
	}

	for _, migration := range migrations {
		if checksum, ok := applied[migration.Name]; ok {
			if checksum != migration.Checksum {
				return fmt.Errorf("applied migration %s was modified after being recorded", migration.Name)
			}
			continue
		}

		if err := applyMigration(ctx, pool, migration); err != nil {
			return err
		}
		fmt.Printf("Applied %s\n", migration.Name)
	}

	return nil
}

func printStatus(ctx context.Context, pool *pgxpool.Pool, migrations []migrationFile) error {
	if err := ensureMigrationTable(ctx, pool); err != nil {
		return err
	}

	applied, err := listApplied(ctx, pool)
	if err != nil {
		return err
	}

	for _, migration := range migrations {
		status := "pending"
		if checksum, ok := applied[migration.Name]; ok {
			if checksum != migration.Checksum {
				status = "modified"
			} else {
				status = "applied"
			}
		}
		fmt.Printf("%-12s %s\n", status, migration.Name)
	}
	return nil
}

func ensureMigrationTable(ctx context.Context, pool interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			checksum TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	return err
}

func databaseLooksInitialized(ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = 'public'
				AND table_name IN ('users', 'posts', 'comments', 'chats')
		)
	`).Scan(&exists)
	return exists, err
}

func listApplied(ctx context.Context, pool interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}) (map[string]string, error) {
	rows, err := pool.Query(ctx, `SELECT version, checksum FROM schema_migrations ORDER BY version ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := map[string]string{}
	for rows.Next() {
		var version string
		var checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			return nil, err
		}
		applied[version] = checksum
	}
	return applied, rows.Err()
}

func stampApplied(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, migrations []migrationFile) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, migration := range migrations {
		if _, err := tx.Exec(ctx,
			`INSERT INTO schema_migrations (version, checksum, applied_at)
			VALUES ($1, $2, NOW())
			ON CONFLICT (version) DO NOTHING`,
			migration.Name, migration.Checksum,
		); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func applyBaseSchema(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}) error {
	sqlBytes, err := os.ReadFile(baseSchema)
	if err != nil {
		return err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func applyMigration(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, migration migrationFile) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, migration.SQL); err != nil {
		return fmt.Errorf("%s: %w", migration.Name, err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO schema_migrations (version, checksum, applied_at) VALUES ($1, $2, NOW())`,
		migration.Name, migration.Checksum,
	); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func loadMigrations() ([]migrationFile, error) {
	paths, err := filepath.Glob(filepath.Join(migrationsDir, "*.sql"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, errors.New("no migration files found")
	}

	migrations := make([]migrationFile, 0, len(paths))
	for _, path := range paths {
		sqlBytes, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(sqlBytes)
		migrations = append(migrations, migrationFile{
			Name:     filepath.Base(path),
			Path:     path,
			Checksum: hex.EncodeToString(sum[:]),
			SQL:      string(sqlBytes),
		})
	}
	return migrations, nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
