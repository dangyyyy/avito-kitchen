package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/dangyyyy/avito-kitchen/internal/platform/domain"
	"github.com/dangyyyy/avito-kitchen/internal/platform/repository"
)

type ClientService struct {
	restaurants *repository.RestaurantRepository
	menu        *repository.MenuRepository
	orders      *repository.OrderRepository
	users       *repository.UserRepository
}

func NewClientService(
	restaurants *repository.RestaurantRepository,
	menu *repository.MenuRepository,
	orders *repository.OrderRepository,
	users *repository.UserRepository,
) *ClientService {
	return &ClientService{restaurants: restaurants, menu: menu, orders: orders, users: users}
}

func (s *ClientService) ListRestaurants(
	ctx context.Context, filter domain.RestaurantFilter,
) ([]domain.Restaurant, error) {
	return s.restaurants.List(ctx, filter)
}

func (s *ClientService) GetRestaurant(ctx context.Context, id uuid.UUID) (domain.Restaurant, error) {
	return s.restaurants.GetByID(ctx, id)
}

// GetMenu returns stop-listed positions too, marked unavailable, so the client
// can grey them out instead of silently losing them from the menu.
func (s *ClientService) GetMenu(
	ctx context.Context, restaurantID uuid.UUID,
) ([]domain.MenuCategoryWithItems, error) {
	if _, err := s.restaurants.GetByID(ctx, restaurantID); err != nil {
		return nil, err
	}

	categories, err := s.menu.ListCategories(ctx, restaurantID)
	if err != nil {
		return nil, err
	}

	items, err := s.menu.ListItems(ctx, restaurantID)
	if err != nil {
		return nil, err
	}

	byCategory := make(map[uuid.UUID][]domain.MenuItem, len(categories))
	for _, item := range items {
		byCategory[item.CategoryID] = append(byCategory[item.CategoryID], item)
	}

	// A menu sync deactivates items but leaves the category row behind, so skip
	// categories that ended up with nothing in them.
	menu := make([]domain.MenuCategoryWithItems, 0, len(categories))

	for _, category := range categories {
		items := byCategory[category.ID]
		if len(items) == 0 {
			continue
		}

		menu = append(menu, domain.MenuCategoryWithItems{Category: category, Items: items})
	}

	return menu, nil
}

func (s *ClientService) SearchDishes(
	ctx context.Context, query string, limit, offset int,
) ([]domain.MenuItemSearchResult, error) {
	return s.menu.SearchItems(ctx, query, limit, offset)
}

func (s *ClientService) ValidateCart(
	ctx context.Context, in repository.PlaceOrderInput,
) (domain.CartValidationResult, error) {
	return s.orders.ValidateCart(ctx, in)
}

func (s *ClientService) PlaceOrder(
	ctx context.Context, in repository.PlaceOrderInput,
) (domain.Order, error) {
	if _, err := s.users.GetByID(ctx, in.UserID); err != nil {
		return domain.Order{}, fmt.Errorf("resolve user: %w", err)
	}

	return s.orders.Place(ctx, in)
}

func (s *ClientService) ListOrders(
	ctx context.Context, userID uuid.UUID, limit, offset int,
) ([]domain.Order, error) {
	return s.orders.ListByUser(ctx, userID, limit, offset)
}

// GetOrder hides someone else's order behind a not-found rather than a forbidden,
// so an identifier cannot be confirmed by probing.
func (s *ClientService) GetOrder(
	ctx context.Context, userID, orderID uuid.UUID,
) (domain.Order, error) {
	order, err := s.orders.GetByID(ctx, orderID)
	if err != nil {
		return domain.Order{}, err
	}

	if order.UserID != userID {
		return domain.Order{}, fmt.Errorf("order %s: %w", orderID, domain.ErrNotFound)
	}

	return order, nil
}

func (s *ClientService) CancelOrder(
	ctx context.Context, userID, orderID uuid.UUID,
) (domain.Order, error) {
	return s.orders.UpdateStatus(ctx, repository.UpdateStatusInput{
		OrderID: orderID,
		To:      domain.StatusCancelled,
		Actor:   domain.ActorUser,
		UserID:  &userID,
	})
}
