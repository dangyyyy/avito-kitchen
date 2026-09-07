package domain

import (
	"time"

	"github.com/google/uuid"
)

type Restaurant struct {
	ID                uuid.UUID
	Name              string
	Description       string
	City              string
	Address           string
	CuisineType       string
	IsAcceptingOrders bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type MenuCategory struct {
	ID           uuid.UUID
	RestaurantID uuid.UUID
	ExternalID   string
	Name         string
	Position     int
}

type MenuItem struct {
	ID           uuid.UUID
	RestaurantID uuid.UUID
	CategoryID   uuid.UUID
	ExternalID   string
	Name         string
	Description  string
	PriceCents   int64
	// IsAvailable is the temporary stop list, IsActive means the item was
	// removed from the menu during a sync.
	IsAvailable bool
	IsActive    bool
}

func (i MenuItem) Orderable() bool {
	return i.IsActive && i.IsAvailable
}

type RestaurantFilter struct {
	City     string
	Cuisine  string
	OpenOnly bool
	Limit    int
	Offset   int
}

// MenuItemSearchResult answers "who sells this dish", so it carries enough of the
// restaurant to render a search hit without a second request.
type MenuItemSearchResult struct {
	Item              MenuItem
	RestaurantName    string
	RestaurantCity    string
	IsAcceptingOrders bool
}
