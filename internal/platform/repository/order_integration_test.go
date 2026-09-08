package repository_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dangyyyy/avito-kitchen/internal/platform/domain"
	"github.com/dangyyyy/avito-kitchen/internal/platform/repository"
)

// TestPlaceOrderBlocksOnTheMenuRowLock is the proof behind the claim that a
// restaurant cannot pull an item into the stop list between validation and
// insert. The lock is held explicitly instead of racing goroutines, so the test
// either demonstrates the guarantee or fails — it cannot pass by luck.
func TestPlaceOrderBlocksOnTheMenuRowLock(t *testing.T) {
	pool := requirePool(t)
	ctx := t.Context()

	restaurantID, itemID, userID := seedRestaurant(t, 45000)
	orders := repository.NewOrderRepository(pool)

	// Take the row the way the restaurant would when editing its menu.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT id FROM menu_items WHERE id = $1 FOR UPDATE`, itemID); err != nil {
		t.Fatalf("lock menu row: %v", err)
	}

	placed := make(chan error, 1)

	go func() {
		_, err := orders.Place(ctx, repository.PlaceOrderInput{
			RestaurantID:    restaurantID,
			UserID:          userID,
			DeliveryAddress: "Ленина 5",
			CustomerPhone:   "+70000000000",
			IdempotencyKey:  "lock-" + uuid.NewString(),
			Items:           []repository.RequestedItem{{MenuItemID: itemID, Quantity: 1}},
		})
		placed <- err
	}()

	select {
	case err := <-placed:
		t.Fatalf("checkout finished while the menu row was locked, returned %v", err)
	case <-time.After(500 * time.Millisecond):
	}

	// Send the item to the stop list, then release the lock.
	if _, err := tx.Exec(ctx, `UPDATE menu_items SET is_available = FALSE WHERE id = $1`, itemID); err != nil {
		t.Fatalf("stop list the item: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	select {
	case err := <-placed:
		var cartErr *domain.CartValidationError
		if !errors.As(err, &cartErr) {
			t.Fatalf("expected a cart validation error after the item was stop-listed, got %v", err)
		}

		if len(cartErr.Problems) != 1 || cartErr.Problems[0].Reason != domain.ItemUnavailable {
			t.Fatalf("expected one 'unavailable' problem, got %+v", cartErr.Problems)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("checkout never returned after the lock was released")
	}

	assertOrderCount(t, userID, 0)
}

// TestPlaceOrderIsIdempotentUnderConcurrency fires the same request from many
// goroutines at once: exactly one order must exist afterwards, and every caller
// must be handed that same order.
func TestPlaceOrderIsIdempotentUnderConcurrency(t *testing.T) {
	pool := requirePool(t)
	ctx := t.Context()

	restaurantID, itemID, userID := seedRestaurant(t, 45000)
	orders := repository.NewOrderRepository(pool)

	const attempts = 12

	input := repository.PlaceOrderInput{
		RestaurantID:    restaurantID,
		UserID:          userID,
		DeliveryAddress: "Ленина 5",
		CustomerPhone:   "+70000000000",
		IdempotencyKey:  "concurrent-" + uuid.NewString(),
		Items:           []repository.RequestedItem{{MenuItemID: itemID, Quantity: 2}},
	}

	var (
		wg    sync.WaitGroup
		ids   = make([]uuid.UUID, attempts)
		errs  = make([]error, attempts)
		start = make(chan struct{})
	)

	for i := range attempts {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()
			<-start

			order, err := orders.Place(ctx, input)
			ids[i], errs[i] = order.ID, err
		}(i)
	}

	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("attempt %d failed: %v", i, err)
		}
	}

	for i, id := range ids {
		if id != ids[0] {
			t.Fatalf("attempt %d returned order %s, expected %s", i, id, ids[0])
		}
	}

	assertOrderCount(t, userID, 1)
}

func TestPlaceOrderCreatesSeparateOrdersForDifferentKeys(t *testing.T) {
	pool := requirePool(t)
	ctx := t.Context()

	restaurantID, itemID, userID := seedRestaurant(t, 45000)
	orders := repository.NewOrderRepository(pool)

	newInput := func() repository.PlaceOrderInput {
		return repository.PlaceOrderInput{
			RestaurantID:    restaurantID,
			UserID:          userID,
			DeliveryAddress: "Ленина 5",
			CustomerPhone:   "+70000000000",
			IdempotencyKey:  "distinct-" + uuid.NewString(),
			Items:           []repository.RequestedItem{{MenuItemID: itemID, Quantity: 1}},
		}
	}

	first, err := orders.Place(ctx, newInput())
	if err != nil {
		t.Fatalf("first order: %v", err)
	}

	second, err := orders.Place(ctx, newInput())
	if err != nil {
		t.Fatalf("second order: %v", err)
	}

	if first.ID == second.ID {
		t.Fatal("different idempotency keys must produce different orders")
	}

	assertOrderCount(t, userID, 2)
}

// TestOrderItemsKeepTheirSnapshot checks that repricing the menu leaves a
// finished order untouched.
func TestOrderItemsKeepTheirSnapshot(t *testing.T) {
	pool := requirePool(t)
	ctx := t.Context()

	restaurantID, itemID, userID := seedRestaurant(t, 45000)
	orders := repository.NewOrderRepository(pool)

	order, err := orders.Place(ctx, repository.PlaceOrderInput{
		RestaurantID:    restaurantID,
		UserID:          userID,
		DeliveryAddress: "Ленина 5",
		CustomerPhone:   "+70000000000",
		IdempotencyKey:  "snapshot-" + uuid.NewString(),
		Items:           []repository.RequestedItem{{MenuItemID: itemID, Quantity: 2}},
	})
	if err != nil {
		t.Fatalf("place order: %v", err)
	}

	if _, err := pool.Exec(ctx,
		`UPDATE menu_items SET name = 'Переименована', price_cents = 99000 WHERE id = $1`, itemID); err != nil {
		t.Fatalf("reprice menu item: %v", err)
	}

	reloaded, err := orders.GetByID(ctx, order.ID)
	if err != nil {
		t.Fatalf("reload order: %v", err)
	}

	if reloaded.TotalPriceCents != 90000 {
		t.Fatalf("expected the order total to stay 90000, got %d", reloaded.TotalPriceCents)
	}

	if reloaded.Items[0].Name != "Позиция" || reloaded.Items[0].PriceCents != 45000 {
		t.Fatalf("expected the snapshot to survive, got %q at %d",
			reloaded.Items[0].Name, reloaded.Items[0].PriceCents)
	}
}

func assertOrderCount(t *testing.T, userID uuid.UUID, want int) {
	t.Helper()

	var got int
	if err := requirePool(t).QueryRow(t.Context(),
		`SELECT count(*) FROM orders WHERE user_id = $1`, userID).Scan(&got); err != nil {
		t.Fatalf("count orders: %v", err)
	}

	if got != want {
		t.Fatalf("expected %d orders, found %d", want, got)
	}
}
