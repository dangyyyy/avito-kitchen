package repository_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// testPool is shared by every test in this package; setupErr records why it is
// missing so the tests can skip with a useful message instead of panicking.
var (
	testPool *pgxpool.Pool
	setupErr error
)

func TestMain(m *testing.M) {
	// os.Exit skips deferred calls, so the whole run lives in a helper that can
	// clean up before returning a code.
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	container, err := startPostgres(ctx)
	if err != nil {
		// Without a database the tests skip rather than fail: not every machine
		// running `go test` has Docker available.
		setupErr = err

		return m.Run()
	}

	defer func() {
		testPool.Close()

		if err := testcontainers.TerminateContainer(container); err != nil {
			fmt.Fprintf(os.Stderr, "terminate container: %v\n", err)
		}
	}()

	return m.Run()
}

func startPostgres(ctx context.Context) (*postgres.PostgresContainer, error) {
	container, err := postgres.Run(ctx, "postgres:17-alpine",
		postgres.WithDatabase("kitchen"),
		postgres.WithUsername("kitchen"),
		postgres.WithPassword("kitchen"),
		testcontainers.WithWaitStrategy(
			wait.ForListeningPort("5432/tcp").WithStartupTimeout(2*time.Minute),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("start postgres: %w", err)
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, fmt.Errorf("connection string: %w", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}

	if err := applySchema(ctx, pool); err != nil {
		return nil, err
	}

	testPool = pool

	return container, nil
}

// applySchema runs the real migration file rather than a copy of the schema, so
// a migration that breaks would break these tests too.
func applySchema(ctx context.Context, pool *pgxpool.Pool) error {
	path := filepath.Join("..", "..", "..", "migrations", "00001_init.sql")

	content, err := os.ReadFile(path) //nolint:gosec // fixed path inside the repository
	if err != nil {
		return fmt.Errorf("read migration: %w", err)
	}

	sql := string(content)

	up, _, found := strings.Cut(sql, "-- +goose Down")
	if !found {
		return fmt.Errorf("migration %s has no Down section marker", path)
	}

	up = strings.ReplaceAll(up, "-- +goose Up", "")

	if _, err := pool.Exec(ctx, up); err != nil {
		return fmt.Errorf("apply migration: %w", err)
	}

	return nil
}

func requirePool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	if setupErr != nil {
		t.Skipf("postgres container unavailable, skipping integration test: %v", setupErr)
	}

	return testPool
}

// seedRestaurant creates an open venue with one available menu item and a user,
// and returns their identifiers.
func seedRestaurant(t *testing.T, priceCents int64) (restaurantID, itemID, userID uuid.UUID) {
	t.Helper()

	ctx := t.Context()
	pool := requirePool(t)

	categoryID := uuid.New()
	restaurantID, itemID, userID = uuid.New(), uuid.New(), uuid.New()

	exec := func(sql string, args ...any) {
		t.Helper()

		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	exec(`INSERT INTO restaurants (id, name, city, address, is_accepting_orders, api_key_hash)
		VALUES ($1, 'Test venue', 'Москва', 'Тестовая 1', TRUE, $2)`,
		restaurantID, "hash-"+restaurantID.String())
	exec(`INSERT INTO menu_categories (id, restaurant_id, external_id, name)
		VALUES ($1, $2, 'cat-1', 'Категория')`, categoryID, restaurantID)
	exec(`INSERT INTO menu_items (id, restaurant_id, category_id, external_id, name, price_cents)
		VALUES ($1, $2, $3, 'item-1', 'Позиция', $4)`, itemID, restaurantID, categoryID, priceCents)
	exec(`INSERT INTO users (id, name, phone) VALUES ($1, 'Тест', '+70000000000')`, userID)

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		_, _ = pool.Exec(cleanupCtx, `DELETE FROM orders WHERE user_id = $1`, userID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM users WHERE id = $1`, userID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM restaurants WHERE id = $1`, restaurantID)
	})

	return restaurantID, itemID, userID
}
