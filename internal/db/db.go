package db

import (
	"context"
	"embed"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed all:migrations
var migrationsFS embed.FS

type DB struct {
	Pool        *pgxpool.Pool
	databaseURL string
}

func New(ctx context.Context, databaseURL string, maxConns int32) (*DB, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if maxConns > 0 {
		config.MaxConns = maxConns
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &DB{Pool: pool, databaseURL: databaseURL}, nil
}

func (d *DB) Migrate() error {
	driver, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("migrate source: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", driver, strings.Replace(d.databaseURL, "postgres://", "pgx5://", 1))
	if err != nil {
		return fmt.Errorf("migrate instance: %w", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		// The pre-merge schema used migrations 001..014; they were consolidated
		// into a single idempotent 001_init. Databases created before the merge
		// carry a schema_migrations version (e.g. 6) that no longer exists in
		// the source files, which makes golang-migrate fail with "no migration
		// found for version N". Their content is exactly the merged 001, so
		// force-align the version to 1 and continue.
		if strings.Contains(err.Error(), "no migration found for version") {
			if ferr := m.Force(1); ferr != nil {
				return fmt.Errorf("migrate force: %w", ferr)
			}
			if err := m.Up(); err != nil && err != migrate.ErrNoChange {
				return fmt.Errorf("migrate up: %w", err)
			}
			return nil
		}
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

func (d *DB) Close() {
	d.Pool.Close()
}
