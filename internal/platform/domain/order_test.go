package domain_test

import (
	"errors"
	"testing"

	"github.com/dangyyyy/avito-kitchen/internal/platform/domain"
)

func TestOrderCanTransition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		from    domain.OrderStatus
		to      domain.OrderStatus
		actor   domain.Actor
		allowed bool
	}{
		{"restaurant accepts a new order", domain.StatusCreated, domain.StatusAccepted, domain.ActorRestaurant, true},
		{"restaurant rejects a new order", domain.StatusCreated, domain.StatusRejected, domain.ActorRestaurant, true},
		{"user cancels before acceptance", domain.StatusCreated, domain.StatusCancelled, domain.ActorUser, true},
		{"restaurant cannot cancel on behalf of the user", domain.StatusCreated, domain.StatusCancelled, domain.ActorRestaurant, false},
		{"user cannot accept an order", domain.StatusCreated, domain.StatusAccepted, domain.ActorUser, false},
		{"user cannot cancel after acceptance", domain.StatusAccepted, domain.StatusCancelled, domain.ActorUser, false},
		{"kitchen starts cooking", domain.StatusAccepted, domain.StatusCooking, domain.ActorRestaurant, true},
		{"cooking finishes", domain.StatusCooking, domain.StatusReady, domain.ActorRestaurant, true},
		{"courier picks the order up", domain.StatusReady, domain.StatusDelivering, domain.ActorRestaurant, true},
		{"delivery completes", domain.StatusDelivering, domain.StatusDelivered, domain.ActorRestaurant, true},
		{"statuses cannot be skipped", domain.StatusCreated, domain.StatusDelivered, domain.ActorRestaurant, false},
		{"delivered order cannot go back to cooking", domain.StatusDelivered, domain.StatusCooking, domain.ActorRestaurant, false},
		{"rejected order is terminal", domain.StatusRejected, domain.StatusAccepted, domain.ActorRestaurant, false},
		{"cancelled order is terminal", domain.StatusCancelled, domain.StatusAccepted, domain.ActorRestaurant, false},
		{"system may perform any valid transition", domain.StatusCreated, domain.StatusCancelled, domain.ActorSystem, true},
		{"system still cannot invent a transition", domain.StatusDelivered, domain.StatusReady, domain.ActorSystem, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			order := &domain.Order{Status: tt.from}

			err := order.CanTransition(tt.to, tt.actor)
			if tt.allowed && err != nil {
				t.Fatalf("expected transition %s -> %s by %s to be allowed, got %v", tt.from, tt.to, tt.actor, err)
			}

			if !tt.allowed {
				if err == nil {
					t.Fatalf("expected transition %s -> %s by %s to be rejected", tt.from, tt.to, tt.actor)
				}

				if !errors.Is(err, domain.ErrTransitionNotAllowed) {
					t.Fatalf("expected ErrTransitionNotAllowed, got %v", err)
				}
			}
		})
	}
}

func TestOrderTransitionMutatesOnlyOnSuccess(t *testing.T) {
	t.Parallel()

	order := &domain.Order{Status: domain.StatusCreated}

	if err := order.Transition(domain.StatusAccepted, domain.ActorRestaurant); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if order.Status != domain.StatusAccepted {
		t.Fatalf("expected status accepted, got %s", order.Status)
	}

	if err := order.Transition(domain.StatusCancelled, domain.ActorUser); err == nil {
		t.Fatal("expected cancellation after acceptance to fail")
	}

	if order.Status != domain.StatusAccepted {
		t.Fatalf("status must stay accepted after a rejected transition, got %s", order.Status)
	}
}

func TestOrderStatusTerminal(t *testing.T) {
	t.Parallel()

	terminal := []domain.OrderStatus{domain.StatusDelivered, domain.StatusRejected, domain.StatusCancelled}
	for _, status := range terminal {
		if !status.Terminal() {
			t.Errorf("expected %s to be terminal", status)
		}
	}

	open := []domain.OrderStatus{
		domain.StatusCreated, domain.StatusAccepted, domain.StatusCooking,
		domain.StatusReady, domain.StatusDelivering,
	}
	for _, status := range open {
		if status.Terminal() {
			t.Errorf("expected %s to be non-terminal", status)
		}
	}
}

func TestTotalCents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		items []domain.OrderItem
		want  int64
	}{
		{"empty order", nil, 0},
		{"single item", []domain.OrderItem{{PriceCents: 45000, Quantity: 1}}, 45000},
		{
			"quantity is multiplied",
			[]domain.OrderItem{{PriceCents: 45000, Quantity: 3}},
			135000,
		},
		{
			"several positions are summed",
			[]domain.OrderItem{
				{PriceCents: 45000, Quantity: 2},
				{PriceCents: 12550, Quantity: 1},
			},
			102550,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := domain.TotalCents(tt.items); got != tt.want {
				t.Fatalf("expected %d, got %d", tt.want, got)
			}
		})
	}
}
