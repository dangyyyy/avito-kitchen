package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dangyyyy/avito-kitchen/internal/platform/domain"
)

type MenuRepository struct {
	pool *pgxpool.Pool
}

func NewMenuRepository(pool *pgxpool.Pool) *MenuRepository {
	return &MenuRepository{pool: pool}
}

const menuItemColumns = `
	id, restaurant_id, category_id, external_id, name, description,
	price_cents, is_available, is_active`

func scanMenuItem(row pgx.CollectableRow) (domain.MenuItem, error) {
	var i domain.MenuItem

	err := row.Scan(
		&i.ID, &i.RestaurantID, &i.CategoryID, &i.ExternalID, &i.Name,
		&i.Description, &i.PriceCents, &i.IsAvailable, &i.IsActive,
	)

	return i, err
}

func (repo *MenuRepository) ListCategories(ctx context.Context, restaurantID uuid.UUID) ([]domain.MenuCategory, error) {
	rows, err := repo.pool.Query(ctx, `
		SELECT id, restaurant_id, external_id, name, position
		FROM menu_categories
		WHERE restaurant_id = $1
		ORDER BY position, name`, restaurantID)
	if err != nil {
		return nil, fmt.Errorf("query categories: %w", err)
	}

	categories, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.MenuCategory, error) {
		var c domain.MenuCategory
		err := row.Scan(&c.ID, &c.RestaurantID, &c.ExternalID, &c.Name, &c.Position)

		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan categories: %w", err)
	}

	return categories, nil
}

func (repo *MenuRepository) ListItems(ctx context.Context, restaurantID uuid.UUID) ([]domain.MenuItem, error) {
	rows, err := repo.pool.Query(ctx, `
		SELECT `+menuItemColumns+`
		FROM menu_items
		WHERE restaurant_id = $1 AND is_active
		ORDER BY name`, restaurantID)
	if err != nil {
		return nil, fmt.Errorf("query menu items: %w", err)
	}

	items, err := pgx.CollectRows(rows, scanMenuItem)
	if err != nil {
		return nil, fmt.Errorf("scan menu items: %w", err)
	}

	return items, nil
}

// SearchItems answers "which restaurant sells this dish". The leading-wildcard
// ILIKE is served by the trigram GIN index on menu_items.name.
func (repo *MenuRepository) SearchItems(
	ctx context.Context, query string, limit, offset int,
) ([]domain.MenuItemSearchResult, error) {
	rows, err := repo.pool.Query(ctx, `
		SELECT
			mi.id, mi.restaurant_id, mi.category_id, mi.external_id, mi.name,
			mi.description, mi.price_cents, mi.is_available, mi.is_active,
			r.name, r.city, r.is_accepting_orders
		FROM menu_items mi
		JOIN restaurants r ON r.id = mi.restaurant_id
		WHERE mi.is_active AND mi.name ILIKE '%' || $1 || '%'
		ORDER BY similarity(mi.name, $1) DESC, mi.name
		LIMIT $2 OFFSET $3`, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("search menu items: %w", err)
	}

	results, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.MenuItemSearchResult, error) {
		var res domain.MenuItemSearchResult

		err := row.Scan(
			&res.Item.ID, &res.Item.RestaurantID, &res.Item.CategoryID,
			&res.Item.ExternalID, &res.Item.Name, &res.Item.Description,
			&res.Item.PriceCents, &res.Item.IsAvailable, &res.Item.IsActive,
			&res.RestaurantName, &res.RestaurantCity, &res.IsAcceptingOrders,
		)

		return res, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan search results: %w", err)
	}

	return results, nil
}

func (repo *MenuRepository) SetItemAvailability(
	ctx context.Context, restaurantID, itemID uuid.UUID, available bool,
) error {
	tag, err := repo.pool.Exec(ctx, `
		UPDATE menu_items
		SET is_available = $3, updated_at = now()
		WHERE restaurant_id = $1 AND id = $2 AND is_active`,
		restaurantID, itemID, available)
	if err != nil {
		return fmt.Errorf("update menu item: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("menu item %s: %w", itemID, domain.ErrNotFound)
	}

	return nil
}
