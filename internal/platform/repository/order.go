package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dangyyyy/avito-kitchen/internal/platform/domain"
)

// errDuplicateOrder signals that a concurrent request already inserted an order
// with the same idempotency key. It never leaves this package.
var errDuplicateOrder = errors.New("duplicate idempotency key")

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type RequestedItem struct {
	MenuItemID uuid.UUID
	Quantity   int
	// ExpectedPriceCents is the price the client saw. Zero means the client did
	// not pin a price and accepts the current one.
	ExpectedPriceCents int64
}

type PlaceOrderInput struct {
	RestaurantID    uuid.UUID
	UserID          uuid.UUID
	DeliveryAddress string
	CustomerPhone   string
	Comment         string
	IdempotencyKey  string
	Items           []RequestedItem
}

type UpdateStatusInput struct {
	OrderID         uuid.UUID
	To              domain.OrderStatus
	Actor           domain.Actor
	RejectionReason string
	// Ownership guards. When set, the order row must match, which keeps one
	// restaurant from touching another restaurant's orders.
	RestaurantID *uuid.UUID
	UserID       *uuid.UUID
}

type OrderRepository struct {
	pool *pgxpool.Pool
}

func NewOrderRepository(pool *pgxpool.Pool) *OrderRepository {
	return &OrderRepository{pool: pool}
}

const orderColumns = `
	id, restaurant_id, user_id, status, total_price_cents, delivery_address,
	customer_phone, comment, rejection_reason, idempotency_key, created_at, updated_at`

func scanOrder(row pgx.CollectableRow) (domain.Order, error) {
	var o domain.Order

	err := row.Scan(
		&o.ID, &o.RestaurantID, &o.UserID, &o.Status, &o.TotalPriceCents,
		&o.DeliveryAddress, &o.CustomerPhone, &o.Comment, &o.RejectionReason,
		&o.IdempotencyKey, &o.CreatedAt, &o.UpdatedAt,
	)

	return o, err
}

type lockedMenuItem struct {
	externalID  string
	name        string
	priceCents  int64
	isAvailable bool
	isActive    bool
}

// Place validates the cart and creates the order atomically. The menu rows are
// locked for the whole transaction, so the restaurant cannot pull an item into
// the stop list or change its price between the check and the insert.
func (repo *OrderRepository) Place(ctx context.Context, in PlaceOrderInput) (domain.Order, error) {
	if len(in.Items) == 0 {
		return domain.Order{}, domain.ErrEmptyOrder
	}

	var placed domain.Order

	err := inTx(ctx, repo.pool, func(tx pgx.Tx) error {
		order, err := placeInTx(ctx, tx, in)
		if err != nil {
			return err
		}

		placed = order

		return nil
	})

	// A concurrent request won the unique index on (user_id, idempotency_key).
	// Its order is the canonical one, so return that instead of failing.
	if errors.Is(err, errDuplicateOrder) {
		return findOrderByIdempotencyKey(ctx, repo.pool, in.UserID, in.IdempotencyKey)
	}

	if err != nil {
		return domain.Order{}, err
	}

	return placed, nil
}

func placeInTx(ctx context.Context, tx pgx.Tx, in PlaceOrderInput) (domain.Order, error) {
	existing, err := findOrderByIdempotencyKey(ctx, tx, in.UserID, in.IdempotencyKey)
	if err == nil {
		return existing, nil
	}

	if !errors.Is(err, domain.ErrNotFound) {
		return domain.Order{}, err
	}

	var accepting bool

	err = tx.QueryRow(ctx,
		`SELECT is_accepting_orders FROM restaurants WHERE id = $1`, in.RestaurantID).Scan(&accepting)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Order{}, fmt.Errorf("restaurant %s: %w", in.RestaurantID, domain.ErrNotFound)
	}

	if err != nil {
		return domain.Order{}, fmt.Errorf("load restaurant: %w", err)
	}

	if !accepting {
		return domain.Order{}, domain.ErrRestaurantClosed
	}

	locked, err := fetchMenuItems(ctx, tx, in, true)
	if err != nil {
		return domain.Order{}, err
	}

	items, problems := buildOrderItems(in, locked)
	if len(problems) > 0 {
		return domain.Order{}, &domain.CartValidationError{Problems: problems}
	}

	return insertOrder(ctx, tx, in, items)
}

// fetchMenuItems loads the referenced menu rows. With forUpdate it also locks
// them for the rest of the transaction; the dry-run path reads without locking.
func fetchMenuItems(
	ctx context.Context, q querier, in PlaceOrderInput, forUpdate bool,
) (map[uuid.UUID]lockedMenuItem, error) {
	ids := make([]uuid.UUID, 0, len(in.Items))
	for _, item := range in.Items {
		ids = append(ids, item.MenuItemID)
	}

	// ORDER BY id gives every transaction the same lock order, so two concurrent
	// orders over overlapping items queue up instead of deadlocking.
	query := `
		SELECT id, external_id, name, price_cents, is_available, is_active
		FROM menu_items
		WHERE restaurant_id = $1 AND id = ANY($2)
		ORDER BY id`
	if forUpdate {
		query += ` FOR UPDATE`
	}

	rows, err := q.Query(ctx, query, in.RestaurantID, ids)
	if err != nil {
		return nil, fmt.Errorf("load menu items: %w", err)
	}

	type keyedItem struct {
		id   uuid.UUID
		item lockedMenuItem
	}

	collected, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (keyedItem, error) {
		var out keyedItem
		err := row.Scan(&out.id, &out.item.externalID, &out.item.name,
			&out.item.priceCents, &out.item.isAvailable, &out.item.isActive)

		return out, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan locked menu items: %w", err)
	}

	locked := make(map[uuid.UUID]lockedMenuItem, len(collected))
	for _, entry := range collected {
		locked[entry.id] = entry.item
	}

	return locked, nil
}

// ValidateCart runs exactly the checks Place would run, but locks nothing and
// writes nothing. Sharing buildOrderItems is the point: the dry-run cannot drift
// away from what actually happens at checkout.
func (repo *OrderRepository) ValidateCart(
	ctx context.Context, in PlaceOrderInput,
) (domain.CartValidationResult, error) {
	if len(in.Items) == 0 {
		return domain.CartValidationResult{}, domain.ErrEmptyOrder
	}

	var accepting bool

	err := repo.pool.QueryRow(ctx,
		`SELECT is_accepting_orders FROM restaurants WHERE id = $1`, in.RestaurantID).Scan(&accepting)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CartValidationResult{}, fmt.Errorf("restaurant %s: %w", in.RestaurantID, domain.ErrNotFound)
	}

	if err != nil {
		return domain.CartValidationResult{}, fmt.Errorf("load restaurant: %w", err)
	}

	if !accepting {
		return domain.CartValidationResult{}, domain.ErrRestaurantClosed
	}

	found, err := fetchMenuItems(ctx, repo.pool, in, false)
	if err != nil {
		return domain.CartValidationResult{}, err
	}

	items, problems := buildOrderItems(in, found)

	return domain.CartValidationResult{
		Valid:           len(problems) == 0,
		TotalPriceCents: domain.TotalCents(items),
		Problems:        problems,
	}, nil
}

func buildOrderItems(
	in PlaceOrderInput, locked map[uuid.UUID]lockedMenuItem,
) ([]domain.OrderItem, []domain.ItemProblem) {
	items := make([]domain.OrderItem, 0, len(in.Items))
	problems := make([]domain.ItemProblem, 0)

	for _, requested := range in.Items {
		menuItem, ok := locked[requested.MenuItemID]
		if !ok || !menuItem.isActive {
			problems = append(problems, domain.ItemProblem{
				MenuItemID: requested.MenuItemID,
				Reason:     domain.ItemNotFound,
			})

			continue
		}

		if !menuItem.isAvailable {
			problems = append(problems, domain.ItemProblem{
				MenuItemID: requested.MenuItemID,
				Name:       menuItem.name,
				Reason:     domain.ItemUnavailable,
			})

			continue
		}

		if requested.ExpectedPriceCents != 0 && requested.ExpectedPriceCents != menuItem.priceCents {
			problems = append(problems, domain.ItemProblem{
				MenuItemID:         requested.MenuItemID,
				Name:               menuItem.name,
				Reason:             domain.ItemPriceChanged,
				ExpectedPriceCents: requested.ExpectedPriceCents,
				ActualPriceCents:   menuItem.priceCents,
			})

			continue
		}

		menuItemID := requested.MenuItemID
		items = append(items, domain.OrderItem{
			ID:             uuid.New(),
			MenuItemID:     &menuItemID,
			ExternalItemID: menuItem.externalID,
			Name:           menuItem.name,
			PriceCents:     menuItem.priceCents,
			Quantity:       requested.Quantity,
		})
	}

	return items, problems
}

func insertOrder(
	ctx context.Context, tx pgx.Tx, in PlaceOrderInput, items []domain.OrderItem,
) (domain.Order, error) {
	order := domain.Order{
		ID:              uuid.New(),
		RestaurantID:    in.RestaurantID,
		UserID:          in.UserID,
		Status:          domain.StatusCreated,
		TotalPriceCents: domain.TotalCents(items),
		DeliveryAddress: in.DeliveryAddress,
		CustomerPhone:   in.CustomerPhone,
		Comment:         in.Comment,
		IdempotencyKey:  in.IdempotencyKey,
		Items:           items,
	}

	err := tx.QueryRow(ctx, `
		INSERT INTO orders (
			id, restaurant_id, user_id, status, total_price_cents,
			delivery_address, customer_phone, comment, idempotency_key)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING created_at, updated_at`,
		order.ID, order.RestaurantID, order.UserID, order.Status, order.TotalPriceCents,
		order.DeliveryAddress, order.CustomerPhone, order.Comment, order.IdempotencyKey,
	).Scan(&order.CreatedAt, &order.UpdatedAt)

	if isUniqueViolation(err) {
		return domain.Order{}, errDuplicateOrder
	}

	if err != nil {
		return domain.Order{}, fmt.Errorf("insert order: %w", err)
	}

	for i := range order.Items {
		order.Items[i].OrderID = order.ID

		_, err = tx.Exec(ctx, `
			INSERT INTO order_items (
				id, order_id, menu_item_id, external_item_id, name, price_cents, quantity)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			order.Items[i].ID, order.ID, order.Items[i].MenuItemID,
			order.Items[i].ExternalItemID, order.Items[i].Name,
			order.Items[i].PriceCents, order.Items[i].Quantity)
		if err != nil {
			return domain.Order{}, fmt.Errorf("insert order item: %w", err)
		}
	}

	if err := insertStatusHistory(ctx, tx, order.ID, "", domain.StatusCreated, domain.ActorUser); err != nil {
		return domain.Order{}, err
	}

	return order, nil
}

func insertStatusHistory(
	ctx context.Context, q querier, orderID uuid.UUID,
	from, to domain.OrderStatus, actor domain.Actor,
) error {
	var fromValue *string
	if from != "" {
		value := string(from)
		fromValue = &value
	}

	_, err := q.Exec(ctx, `
		INSERT INTO order_status_history (order_id, from_status, to_status, changed_by)
		VALUES ($1, $2, $3, $4)`, orderID, fromValue, to, actor)
	if err != nil {
		return fmt.Errorf("insert status history: %w", err)
	}

	return nil
}

func findOrderByIdempotencyKey(
	ctx context.Context, q querier, userID uuid.UUID, key string,
) (domain.Order, error) {
	rows, err := q.Query(ctx,
		`SELECT `+orderColumns+` FROM orders WHERE user_id = $1 AND idempotency_key = $2`,
		userID, key)
	if err != nil {
		return domain.Order{}, fmt.Errorf("query order by idempotency key: %w", err)
	}

	order, err := pgx.CollectExactlyOneRow(rows, scanOrder)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Order{}, domain.ErrNotFound
	}

	if err != nil {
		return domain.Order{}, fmt.Errorf("scan order: %w", err)
	}

	order.Items, err = loadOrderItems(ctx, q, order.ID)
	if err != nil {
		return domain.Order{}, err
	}

	return order, nil
}

func loadOrderItems(ctx context.Context, q querier, orderIDs ...uuid.UUID) ([]domain.OrderItem, error) {
	rows, err := q.Query(ctx, `
		SELECT id, order_id, menu_item_id, external_item_id, name, price_cents, quantity
		FROM order_items
		WHERE order_id = ANY($1)
		ORDER BY name`, orderIDs)
	if err != nil {
		return nil, fmt.Errorf("query order items: %w", err)
	}

	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.OrderItem, error) {
		var item domain.OrderItem
		err := row.Scan(&item.ID, &item.OrderID, &item.MenuItemID,
			&item.ExternalItemID, &item.Name, &item.PriceCents, &item.Quantity)

		return item, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan order items: %w", err)
	}

	return items, nil
}

func (repo *OrderRepository) GetByID(ctx context.Context, id uuid.UUID) (domain.Order, error) {
	rows, err := repo.pool.Query(ctx, `SELECT `+orderColumns+` FROM orders WHERE id = $1`, id)
	if err != nil {
		return domain.Order{}, fmt.Errorf("query order: %w", err)
	}

	order, err := pgx.CollectExactlyOneRow(rows, scanOrder)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Order{}, fmt.Errorf("order %s: %w", id, domain.ErrNotFound)
	}

	if err != nil {
		return domain.Order{}, fmt.Errorf("scan order: %w", err)
	}

	order.Items, err = loadOrderItems(ctx, repo.pool, order.ID)
	if err != nil {
		return domain.Order{}, err
	}

	return order, nil
}

func (repo *OrderRepository) ListByUser(
	ctx context.Context, userID uuid.UUID, limit, offset int,
) ([]domain.Order, error) {
	rows, err := repo.pool.Query(ctx, `
		SELECT `+orderColumns+`
		FROM orders
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query user orders: %w", err)
	}

	return repo.collectWithItems(ctx, rows)
}

func (repo *OrderRepository) ListByRestaurantAndStatus(
	ctx context.Context, restaurantID uuid.UUID, status domain.OrderStatus, limit int,
) ([]domain.Order, error) {
	rows, err := repo.pool.Query(ctx, `
		SELECT `+orderColumns+`
		FROM orders
		WHERE restaurant_id = $1 AND ($2 = '' OR status = $2)
		ORDER BY created_at
		LIMIT $3`, restaurantID, status, limit)
	if err != nil {
		return nil, fmt.Errorf("query restaurant orders: %w", err)
	}

	return repo.collectWithItems(ctx, rows)
}

// collectWithItems loads the items of every order in one extra query instead of
// one query per order.
func (repo *OrderRepository) collectWithItems(ctx context.Context, rows pgx.Rows) ([]domain.Order, error) {
	orders, err := pgx.CollectRows(rows, scanOrder)
	if err != nil {
		return nil, fmt.Errorf("scan orders: %w", err)
	}

	if len(orders) == 0 {
		return orders, nil
	}

	ids := make([]uuid.UUID, 0, len(orders))
	for _, order := range orders {
		ids = append(ids, order.ID)
	}

	items, err := loadOrderItems(ctx, repo.pool, ids...)
	if err != nil {
		return nil, err
	}

	byOrder := make(map[uuid.UUID][]domain.OrderItem, len(orders))
	for _, item := range items {
		byOrder[item.OrderID] = append(byOrder[item.OrderID], item)
	}

	for i := range orders {
		orders[i].Items = byOrder[orders[i].ID]
	}

	return orders, nil
}

// UpdateStatus reloads the order under a row lock, asks the domain whether the
// transition is allowed, and records it in the history within one transaction.
func (repo *OrderRepository) UpdateStatus(ctx context.Context, in UpdateStatusInput) (domain.Order, error) {
	var updated domain.Order

	err := inTx(ctx, repo.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT `+orderColumns+` FROM orders WHERE id = $1 FOR UPDATE`, in.OrderID)
		if err != nil {
			return fmt.Errorf("lock order: %w", err)
		}

		order, err := pgx.CollectExactlyOneRow(rows, scanOrder)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("order %s: %w", in.OrderID, domain.ErrNotFound)
		}

		if err != nil {
			return fmt.Errorf("scan order: %w", err)
		}

		if in.RestaurantID != nil && order.RestaurantID != *in.RestaurantID {
			return fmt.Errorf("order %s: %w", in.OrderID, domain.ErrNotFound)
		}

		if in.UserID != nil && order.UserID != *in.UserID {
			return fmt.Errorf("order %s: %w", in.OrderID, domain.ErrNotFound)
		}

		from := order.Status
		if err := order.Transition(in.To, in.Actor); err != nil {
			return err
		}

		if in.RejectionReason != "" {
			order.RejectionReason = in.RejectionReason
		}

		err = tx.QueryRow(ctx, `
			UPDATE orders
			SET status = $2, rejection_reason = $3, updated_at = now()
			WHERE id = $1
			RETURNING updated_at`,
			order.ID, order.Status, order.RejectionReason).Scan(&order.UpdatedAt)
		if err != nil {
			return fmt.Errorf("update order status: %w", err)
		}

		if err := insertStatusHistory(ctx, tx, order.ID, from, order.Status, in.Actor); err != nil {
			return err
		}

		order.Items, err = loadOrderItems(ctx, tx, order.ID)
		if err != nil {
			return err
		}

		updated = order

		return nil
	})
	if err != nil {
		return domain.Order{}, err
	}

	return updated, nil
}
