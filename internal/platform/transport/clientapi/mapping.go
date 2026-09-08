package clientapi

import (
	"github.com/google/uuid"

	"github.com/dangyyyy/avito-kitchen/internal/platform/domain"
	"github.com/dangyyyy/avito-kitchen/internal/platform/repository"
)

const (
	defaultLimit  = 20
	defaultOffset = 0
)

func ptr[T any](v T) *T {
	return &v
}

// optional turns an empty string into an absent JSON field, so clients do not
// have to distinguish "" from missing.
func optional(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

func pageOrDefault(limit *Limit, offset *Offset) (int, int) {
	page, from := defaultLimit, defaultOffset
	if limit != nil {
		page = *limit
	}

	if offset != nil {
		from = *offset
	}

	return page, from
}

func toRestaurant(r domain.Restaurant) Restaurant {
	return Restaurant{
		Id:                r.ID,
		Name:              r.Name,
		Description:       optional(r.Description),
		City:              r.City,
		Address:           r.Address,
		CuisineType:       r.CuisineType,
		IsAcceptingOrders: r.IsAcceptingOrders,
	}
}

func toRestaurants(list []domain.Restaurant) []Restaurant {
	out := make([]Restaurant, 0, len(list))
	for _, r := range list {
		out = append(out, toRestaurant(r))
	}

	return out
}

func toMenuItem(i domain.MenuItem) MenuItem {
	return MenuItem{
		Id:          i.ID,
		Name:        i.Name,
		Description: optional(i.Description),
		PriceCents:  i.PriceCents,
		IsAvailable: i.IsAvailable,
	}
}

func toMenu(restaurantID uuid.UUID, categories []domain.MenuCategoryWithItems) Menu {
	out := make([]MenuCategory, 0, len(categories))

	for _, c := range categories {
		items := make([]MenuItem, 0, len(c.Items))
		for _, item := range c.Items {
			items = append(items, toMenuItem(item))
		}

		out = append(out, MenuCategory{Id: c.Category.ID, Name: c.Category.Name, Items: items})
	}

	return Menu{RestaurantId: restaurantID, Categories: out}
}

func toSearchHits(results []domain.MenuItemSearchResult) []MenuSearchHit {
	out := make([]MenuSearchHit, 0, len(results))
	for _, r := range results {
		out = append(out, MenuSearchHit{
			Item:              toMenuItem(r.Item),
			RestaurantId:      r.Item.RestaurantID,
			RestaurantName:    r.RestaurantName,
			RestaurantCity:    r.RestaurantCity,
			IsAcceptingOrders: r.IsAcceptingOrders,
		})
	}

	return out
}

func toItemProblems(problems []domain.ItemProblem) []ItemProblem {
	out := make([]ItemProblem, 0, len(problems))

	for _, p := range problems {
		problem := ItemProblem{
			MenuItemId: p.MenuItemID,
			Name:       optional(p.Name),
			Reason:     ItemProblemReason(p.Reason),
		}

		if p.Reason == domain.ItemPriceChanged {
			problem.ExpectedPriceCents = ptr(p.ExpectedPriceCents)
			problem.ActualPriceCents = ptr(p.ActualPriceCents)
		}

		out = append(out, problem)
	}

	return out
}

func toOrder(o domain.Order) Order {
	items := make([]OrderItem, 0, len(o.Items))
	for _, item := range o.Items {
		items = append(items, OrderItem{
			MenuItemId: item.MenuItemID,
			Name:       item.Name,
			PriceCents: item.PriceCents,
			Quantity:   item.Quantity,
		})
	}

	order := Order{
		Id:              o.ID,
		RestaurantId:    o.RestaurantID,
		Status:          OrderStatus(o.Status),
		TotalPriceCents: o.TotalPriceCents,
		DeliveryAddress: o.DeliveryAddress,
		CustomerPhone:   o.CustomerPhone,
		Comment:         optional(o.Comment),
		RejectionReason: optional(o.RejectionReason),
		Items:           items,
		CreatedAt:       o.CreatedAt,
	}

	if !o.UpdatedAt.IsZero() {
		order.UpdatedAt = ptr(o.UpdatedAt)
	}

	return order
}

func toOrders(list []domain.Order) []Order {
	out := make([]Order, 0, len(list))
	for _, o := range list {
		out = append(out, toOrder(o))
	}

	return out
}

func toRequestedItems(items []CartItem) []repository.RequestedItem {
	out := make([]repository.RequestedItem, 0, len(items))

	for _, item := range items {
		requested := repository.RequestedItem{
			MenuItemID: item.MenuItemId,
			Quantity:   item.Quantity,
		}

		if item.ExpectedPriceCents != nil {
			requested.ExpectedPriceCents = *item.ExpectedPriceCents
		}

		out = append(out, requested)
	}

	return out
}
