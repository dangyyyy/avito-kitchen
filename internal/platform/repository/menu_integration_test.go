package repository_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/dangyyyy/avito-kitchen/internal/platform/domain"
	"github.com/dangyyyy/avito-kitchen/internal/platform/repository"
)

func fullMenu() []domain.MenuSyncCategory {
	return []domain.MenuSyncCategory{{
		ExternalID: "cat-1",
		Name:       "Категория",
		Position:   1,
		Items: []domain.MenuSyncItem{
			{ExternalID: "item-1", Name: "Позиция", PriceCents: 45000, IsAvailable: true},
			{ExternalID: "item-2", Name: "Вторая позиция", PriceCents: 30000, IsAvailable: true},
		},
	}}
}

// TestMenuSyncIsIdempotent replays the same payload: the second run must not
// duplicate anything, and identifiers already handed out must stay valid.
func TestMenuSyncIsIdempotent(t *testing.T) {
	pool := requirePool(t)
	ctx := t.Context()

	restaurantID, itemID, _ := seedRestaurant(t, 45000)
	menu := repository.NewMenuRepository(pool)

	first, err := menu.Sync(ctx, restaurantID, fullMenu())
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}

	if first.ItemsUpserted != 2 || first.ItemsDeactivated != 0 {
		t.Fatalf("first sync: upserted=%d deactivated=%d, want 2 and 0",
			first.ItemsUpserted, first.ItemsDeactivated)
	}

	second, err := menu.Sync(ctx, restaurantID, fullMenu())
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if second != first {
		t.Fatalf("replaying the same menu changed the outcome: %+v then %+v", first, second)
	}

	items, err := menu.ListItems(ctx, restaurantID)
	if err != nil {
		t.Fatalf("list items: %v", err)
	}

	if len(items) != 2 {
		t.Fatalf("expected 2 items after two identical syncs, got %d", len(items))
	}

	// The seeded item was addressed by the same external id, so it must have been
	// updated in place rather than replaced by a new row.
	if !containsID(items, itemID) {
		t.Fatalf("upsert replaced the existing row: item %s is gone", itemID)
	}
}

// TestMenuSyncDeactivatesRemovedItems checks that dropping an item hides it from
// clients without deleting the row past orders point at.
func TestMenuSyncDeactivatesRemovedItems(t *testing.T) {
	pool := requirePool(t)
	ctx := t.Context()

	restaurantID, _, _ := seedRestaurant(t, 45000)
	menu := repository.NewMenuRepository(pool)

	if _, err := menu.Sync(ctx, restaurantID, fullMenu()); err != nil {
		t.Fatalf("initial sync: %v", err)
	}

	shrunk := fullMenu()
	shrunk[0].Items = shrunk[0].Items[:1]

	result, err := menu.Sync(ctx, restaurantID, shrunk)
	if err != nil {
		t.Fatalf("shrinking sync: %v", err)
	}

	if result.ItemsDeactivated != 1 {
		t.Fatalf("expected 1 deactivated item, got %d", result.ItemsDeactivated)
	}

	items, err := menu.ListItems(ctx, restaurantID)
	if err != nil {
		t.Fatalf("list items: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("expected 1 visible item, got %d", len(items))
	}

	var rows int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM menu_items WHERE restaurant_id = $1`, restaurantID).Scan(&rows); err != nil {
		t.Fatalf("count rows: %v", err)
	}

	if rows != 2 {
		t.Fatalf("the removed item must stay in the table, found %d rows", rows)
	}
}

func containsID(items []domain.MenuItem, id uuid.UUID) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}

	return false
}
