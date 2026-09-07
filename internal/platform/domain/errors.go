package domain

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

var (
	ErrNotFound             = errors.New("not found")
	ErrRestaurantClosed     = errors.New("restaurant is not accepting orders")
	ErrEmptyOrder           = errors.New("order must contain at least one item")
	ErrTransitionNotAllowed = errors.New("order status transition is not allowed")
)

type ItemProblemReason string

const (
	ItemNotFound     ItemProblemReason = "not_found"
	ItemUnavailable  ItemProblemReason = "unavailable"
	ItemPriceChanged ItemProblemReason = "price_changed"
)

type ItemProblem struct {
	MenuItemID         uuid.UUID
	Name               string
	Reason             ItemProblemReason
	ExpectedPriceCents int64
	ActualPriceCents   int64
}

// CartValidationError reports every problem found in a single pass so the client
// can fix the whole cart at once instead of retrying item by item.
type CartValidationError struct {
	Problems []ItemProblem
}

func (e *CartValidationError) Error() string {
	parts := make([]string, 0, len(e.Problems))
	for _, p := range e.Problems {
		parts = append(parts, fmt.Sprintf("%s: %s", p.MenuItemID, p.Reason))
	}

	return "cart validation failed: " + strings.Join(parts, ", ")
}
