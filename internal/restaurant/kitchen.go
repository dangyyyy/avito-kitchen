package restaurant

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/dangyyyy/avito-kitchen/internal/restaurant/partnerclient"
)

const (
	bootstrapAttempts = 30
	bootstrapDelay    = 2 * time.Second
	orderPageSize     = 100
)

// nextStatus drives the simulated kitchen: one step per poll tick.
var nextStatus = map[partnerclient.PartnerOrderStatus]partnerclient.PartnerOrderStatus{
	partnerclient.Accepted:   partnerclient.Cooking,
	partnerclient.Cooking:    partnerclient.Ready,
	partnerclient.Ready:      partnerclient.Delivering,
	partnerclient.Delivering: partnerclient.Delivered,
}

type Kitchen struct {
	client   *partnerclient.ClientWithResponses
	interval time.Duration

	mu           sync.Mutex
	rejectNext   bool
	rejectReason string
}

func NewKitchen(client *partnerclient.ClientWithResponses, interval time.Duration) *Kitchen {
	return &Kitchen{client: client, interval: interval}
}

// ArmRejection makes the next incoming order be refused. Rejection is triggered
// deliberately through the control API rather than at random, so a demo run is
// reproducible.
func (k *Kitchen) ArmRejection(reason string) {
	k.mu.Lock()
	defer k.mu.Unlock()

	k.rejectNext = true
	k.rejectReason = reason
}

func (k *Kitchen) takeRejection() (string, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()

	if !k.rejectNext {
		return "", false
	}

	k.rejectNext = false

	return k.rejectReason, true
}

func (k *Kitchen) Run(ctx context.Context) error {
	if err := k.bootstrap(ctx); err != nil {
		return err
	}

	ticker := time.NewTicker(k.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			// Existing orders move first, so an order accepted on this tick waits
			// until the next one before it starts cooking.
			k.advanceInFlight(ctx)
			k.pickUpNewOrders(ctx)
		}
	}
}

// bootstrap waits for the platform, publishes the menu and opens the venue. The
// retry loop exists because the api container may still be starting.
func (k *Kitchen) bootstrap(ctx context.Context) error {
	var lastErr error

	for attempt := 1; attempt <= bootstrapAttempts; attempt++ {
		profile, err := k.client.GetProfileWithResponse(ctx)
		if err == nil && profile.StatusCode() == http.StatusOK && profile.JSON200 != nil {
			slog.Info("connected to platform",
				"restaurant", profile.JSON200.Name, "id", profile.JSON200.Id)

			lastErr = nil

			break
		}

		lastErr = err
		if lastErr == nil {
			lastErr = fmt.Errorf("unexpected status %d", profile.StatusCode())
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(bootstrapDelay):
		}
	}

	if lastErr != nil {
		return fmt.Errorf("connect to platform: %w", lastErr)
	}

	sync, err := k.client.SyncMenuWithResponse(ctx, DemoMenu())
	if err != nil {
		return fmt.Errorf("sync menu: %w", err)
	}

	if sync.StatusCode() != http.StatusOK || sync.JSON200 == nil {
		return fmt.Errorf("sync menu: unexpected status %d", sync.StatusCode())
	}

	slog.Info("menu published",
		"categories", sync.JSON200.Categories,
		"items_upserted", sync.JSON200.ItemsUpserted,
		"items_deactivated", sync.JSON200.ItemsDeactivated)

	if err := k.SetAcceptingOrders(ctx, true); err != nil {
		return fmt.Errorf("open the venue: %w", err)
	}

	slog.Info("venue is open and polling for orders", "interval", k.interval)

	return nil
}

func (k *Kitchen) pickUpNewOrders(ctx context.Context) {
	created := partnerclient.Created

	resp, err := k.client.ListPartnerOrdersWithResponse(ctx, &partnerclient.ListPartnerOrdersParams{
		Status: &created,
		Limit:  ptr(orderPageSize),
	})
	if err != nil {
		slog.Warn("poll new orders", "error", err)

		return
	}

	if resp.JSON200 == nil {
		slog.Warn("poll new orders", "status", resp.StatusCode())

		return
	}

	for _, order := range resp.JSON200.Items {
		if reason, armed := k.takeRejection(); armed {
			k.reject(ctx, order.Id, reason)

			continue
		}

		k.accept(ctx, order.Id)
	}
}

func (k *Kitchen) accept(ctx context.Context, orderID partnerclient.OrderId) {
	resp, err := k.client.AcceptOrderWithResponse(ctx, orderID)
	if err != nil {
		slog.Warn("accept order", "order_id", orderID, "error", err)

		return
	}

	if resp.StatusCode() != http.StatusOK {
		// A 409 here is normal: the customer may have cancelled in between.
		slog.Info("order not accepted", "order_id", orderID, "status", resp.StatusCode())

		return
	}

	slog.Info("order accepted", "order_id", orderID)
}

func (k *Kitchen) reject(ctx context.Context, orderID partnerclient.OrderId, reason string) {
	resp, err := k.client.RejectOrderWithResponse(ctx, orderID,
		partnerclient.RejectOrderJSONRequestBody{Reason: reason})
	if err != nil {
		slog.Warn("reject order", "order_id", orderID, "error", err)

		return
	}

	slog.Info("order rejected", "order_id", orderID, "reason", reason, "status", resp.StatusCode())
}

func (k *Kitchen) advanceInFlight(ctx context.Context) {
	resp, err := k.client.ListPartnerOrdersWithResponse(ctx, &partnerclient.ListPartnerOrdersParams{
		Limit: ptr(orderPageSize),
	})
	if err != nil {
		slog.Warn("poll in-flight orders", "error", err)

		return
	}

	if resp.JSON200 == nil {
		slog.Warn("poll in-flight orders", "status", resp.StatusCode())

		return
	}

	for _, order := range resp.JSON200.Items {
		next, ok := nextStatus[order.Status]
		if !ok {
			continue
		}

		advance, err := k.client.AdvanceOrderStatusWithResponse(ctx, order.Id,
			partnerclient.AdvanceOrderStatusJSONRequestBody{Status: next})
		if err != nil {
			slog.Warn("advance order", "order_id", order.Id, "error", err)

			continue
		}

		if advance.StatusCode() != http.StatusOK {
			slog.Info("order not advanced",
				"order_id", order.Id, "to", next, "status", advance.StatusCode())

			continue
		}

		slog.Info("order advanced", "order_id", order.Id, "from", order.Status, "to", next)
	}
}

func (k *Kitchen) SetAcceptingOrders(ctx context.Context, accepting bool) error {
	resp, err := k.client.UpdateSettingsWithResponse(ctx,
		partnerclient.UpdateSettingsJSONRequestBody{IsAcceptingOrders: accepting})
	if err != nil {
		return err
	}

	if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode())
	}

	return nil
}

var ErrItemNotFound = errors.New("menu item not found")

func (k *Kitchen) SetItemAvailability(ctx context.Context, externalID string, available bool) error {
	resp, err := k.client.SetItemAvailabilityWithResponse(ctx, externalID,
		partnerclient.SetItemAvailabilityJSONRequestBody{IsAvailable: available})
	if err != nil {
		return err
	}

	if resp.StatusCode() == http.StatusNotFound {
		return ErrItemNotFound
	}

	if resp.StatusCode() != http.StatusNoContent {
		return fmt.Errorf("unexpected status %d", resp.StatusCode())
	}

	return nil
}
