//go:build integration

package database

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"
)

func TestConcurrentMigrateAndVerifySchema(t *testing.T) {
	databaseURL := os.Getenv("NIMBUS_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("NIMBUS_TEST_DATABASE_URL is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer pool.Close()

	var wg sync.WaitGroup
	errors := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errors <- Migrate(ctx, pool)
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent migrate: %v", err)
		}
	}
	if err := VerifySchema(ctx, pool); err != nil {
		t.Fatalf("verify schema: %v", err)
	}

	var missingChecksums int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations WHERE checksum IS NULL OR checksum = ''").Scan(&missingChecksums); err != nil {
		t.Fatalf("query migration checksums: %v", err)
	}
	if missingChecksums != 0 {
		t.Fatalf("found %d migrations without checksums", missingChecksums)
	}
}
