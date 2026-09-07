package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type OrderStatus string

const (
	StatusCreated    OrderStatus = "created"
	StatusAccepted   OrderStatus = "accepted"
	StatusCooking    OrderStatus = "cooking"
	StatusReady      OrderStatus = "ready"
	StatusDelivering OrderStatus = "delivering"
	StatusDelivered  OrderStatus = "delivered"
	StatusRejected   OrderStatus = "rejected"
	StatusCancelled  OrderStatus = "cancelled"
)

type Actor string

const (
	ActorUser       Actor = "user"
	ActorRestaurant Actor = "restaurant"
	ActorSystem     Actor = "system"
)

type Order struct {
	ID              uuid.UUID
	RestaurantID    uuid.UUID
	UserID          uuid.UUID
	Status          OrderStatus
	TotalPriceCents int64
	DeliveryAddress string
	CustomerPhone   string
	Comment         string
	RejectionReason string
	IdempotencyKey  string
	Items           []OrderItem
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type OrderItem struct {
	ID         uuid.UUID
	OrderID    uuid.UUID
	MenuItemID *uuid.UUID
	// Name and PriceCents are a snapshot taken when the order was placed, so a
	// later menu change cannot rewrite the history of a finished order.
	ExternalItemID string
	Name           string
	PriceCents     int64
	Quantity       int
}

type transition struct {
	to    OrderStatus
	actor Actor
}

var orderTransitions = map[OrderStatus][]transition{
	StatusCreated: {
		{to: StatusAccepted, actor: ActorRestaurant},
		{to: StatusRejected, actor: ActorRestaurant},
		{to: StatusCancelled, actor: ActorUser},
	},
	StatusAccepted: {
		{to: StatusCooking, actor: ActorRestaurant},
		{to: StatusRejected, actor: ActorRestaurant},
	},
	StatusCooking:    {{to: StatusReady, actor: ActorRestaurant}},
	StatusReady:      {{to: StatusDelivering, actor: ActorRestaurant}},
	StatusDelivering: {{to: StatusDelivered, actor: ActorRestaurant}},
	StatusDelivered:  {},
	StatusRejected:   {},
	StatusCancelled:  {},
}

func (s OrderStatus) Terminal() bool {
	return len(orderTransitions[s]) == 0
}

func (s OrderStatus) Valid() bool {
	_, ok := orderTransitions[s]

	return ok
}

// CanTransition reports whether actor is allowed to move the order to the given
// status. All status changes go through here so the rules live in one place.
func (o *Order) CanTransition(to OrderStatus, actor Actor) error {
	allowed, ok := orderTransitions[o.Status]
	if !ok {
		return fmt.Errorf("%w: unknown current status %q", ErrTransitionNotAllowed, o.Status)
	}

	actorMismatch := false

	for _, t := range allowed {
		if t.to != to {
			continue
		}

		if t.actor == actor || actor == ActorSystem {
			return nil
		}

		actorMismatch = true
	}

	if actorMismatch {
		return fmt.Errorf("%w: %s cannot move order from %q to %q",
			ErrTransitionNotAllowed, actor, o.Status, to)
	}

	return fmt.Errorf("%w: cannot move order from %q to %q",
		ErrTransitionNotAllowed, o.Status, to)
}

func (o *Order) Transition(to OrderStatus, actor Actor) error {
	if err := o.CanTransition(to, actor); err != nil {
		return err
	}

	o.Status = to

	return nil
}

func TotalCents(items []OrderItem) int64 {
	var total int64
	for _, item := range items {
		total += item.PriceCents * int64(item.Quantity)
	}

	return total
}
