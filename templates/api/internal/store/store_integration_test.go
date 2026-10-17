//go:build integration

package store

import (
	"context"
	"os"
	"testing"
)

// Run with a database: YGG_DATABASE_URL=postgres://postgres@postgres:5432/app?sslmode=disable
// go test -tags integration ./... (the factory's integration check does it, in a sandbox that starts one).
func TestTheStoreMigratesAndAnswersQueries(t *testing.T) {
	url := os.Getenv("YGG_DATABASE_URL")
	if url == "" {
		t.Skip("set YGG_DATABASE_URL to run this test")
	}
	ctx := context.Background()
	pool, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrating a second time = %v; applied migrations are skipped", err)
	}
	q := New(pool)
	if err := q.SetInfo(ctx, SetInfoParams{Key: "greeting", Value: "hello"}); err != nil {
		t.Fatal(err)
	}
	if got, err := q.GetInfo(ctx, "greeting"); err != nil || got != "hello" {
		t.Errorf("GetInfo() = %q, %v; want hello", got, err)
	}
}
