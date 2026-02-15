package database

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

var Pool *pgxpool.Pool

// Connect creates a connection pool
func Connect() error {

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return fmt.Errorf("DATABASE_URL not set")
	}

	// Parse config
	config, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return fmt.Errorf("unable to parse DATABASE_URL: %w", err)
	}

	// Configure pool
	config.MaxConns = 20
	config.MinConns = 5

	// Create pool
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		return fmt.Errorf("unable to create connection pool: %w", err)
	}

	// Test connection
	if err := pool.Ping(context.Background()); err != nil {
		return fmt.Errorf("unable to ping database: %w", err)
	}

	Pool = pool
	return nil
}

// Close closes the connection pool
func Close() {
	if Pool != nil {
		Pool.Close()
	}
}
