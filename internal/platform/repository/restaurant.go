package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dangyyyy/avito-kitchen/internal/platform/domain"
)

type RestaurantRepository struct {
	pool *pgxpool.Pool
}

func NewRestaurantRepository(pool *pgxpool.Pool) *RestaurantRepository {
	return &RestaurantRepository{pool: pool}
}

const restaurantColumns = `
	id, name, description, city, address, cuisine_type,
	is_accepting_orders, created_at, updated_at`

func scanRestaurant(row pgx.CollectableRow) (domain.Restaurant, error) {
	var r domain.Restaurant

	err := row.Scan(
		&r.ID, &r.Name, &r.Description, &r.City, &r.Address, &r.CuisineType,
		&r.IsAcceptingOrders, &r.CreatedAt, &r.UpdatedAt,
	)

	return r, err
}

func (repo *RestaurantRepository) GetByID(ctx context.Context, id uuid.UUID) (domain.Restaurant, error) {
	rows, err := repo.pool.Query(ctx,
		`SELECT `+restaurantColumns+` FROM restaurants WHERE id = $1`, id)
	if err != nil {
		return domain.Restaurant{}, fmt.Errorf("query restaurant: %w", err)
	}

	restaurant, err := pgx.CollectExactlyOneRow(rows, scanRestaurant)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Restaurant{}, fmt.Errorf("restaurant %s: %w", id, domain.ErrNotFound)
	}

	if err != nil {
		return domain.Restaurant{}, fmt.Errorf("scan restaurant: %w", err)
	}

	return restaurant, nil
}

func (repo *RestaurantRepository) GetByAPIKeyHash(ctx context.Context, hash string) (domain.Restaurant, error) {
	rows, err := repo.pool.Query(ctx,
		`SELECT `+restaurantColumns+` FROM restaurants WHERE api_key_hash = $1`, hash)
	if err != nil {
		return domain.Restaurant{}, fmt.Errorf("query restaurant by api key: %w", err)
	}

	restaurant, err := pgx.CollectExactlyOneRow(rows, scanRestaurant)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Restaurant{}, domain.ErrNotFound
	}

	if err != nil {
		return domain.Restaurant{}, fmt.Errorf("scan restaurant: %w", err)
	}

	return restaurant, nil
}

// List applies every filter in a single statement; empty filter fields are
// skipped by the NULLIF checks instead of building SQL by hand.
func (repo *RestaurantRepository) List(ctx context.Context, f domain.RestaurantFilter) ([]domain.Restaurant, error) {
	rows, err := repo.pool.Query(ctx, `
		SELECT `+restaurantColumns+`
		FROM restaurants
		WHERE ($1 = '' OR city = $1)
		  AND ($2 = '' OR cuisine_type = $2)
		  AND (NOT $3 OR is_accepting_orders)
		ORDER BY name
		LIMIT $4 OFFSET $5`,
		f.City, f.Cuisine, f.OpenOnly, f.Limit, f.Offset)
	if err != nil {
		return nil, fmt.Errorf("query restaurants: %w", err)
	}

	restaurants, err := pgx.CollectRows(rows, scanRestaurant)
	if err != nil {
		return nil, fmt.Errorf("scan restaurants: %w", err)
	}

	return restaurants, nil
}

func (repo *RestaurantRepository) SetAcceptingOrders(ctx context.Context, id uuid.UUID, accepting bool) error {
	tag, err := repo.pool.Exec(ctx, `
		UPDATE restaurants
		SET is_accepting_orders = $2, updated_at = now()
		WHERE id = $1`, id, accepting)
	if err != nil {
		return fmt.Errorf("update restaurant: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("restaurant %s: %w", id, domain.ErrNotFound)
	}

	return nil
}
