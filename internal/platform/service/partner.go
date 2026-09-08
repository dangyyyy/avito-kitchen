package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/dangyyyy/avito-kitchen/internal/pkg/apikey"
	"github.com/dangyyyy/avito-kitchen/internal/platform/domain"
	"github.com/dangyyyy/avito-kitchen/internal/platform/repository"
)

type PartnerService struct {
	restaurants *repository.RestaurantRepository
	menu        *repository.MenuRepository
	orders      *repository.OrderRepository
}

func NewPartnerService(
	restaurants *repository.RestaurantRepository,
	menu *repository.MenuRepository,
	orders *repository.OrderRepository,
) *PartnerService {
	return &PartnerService{restaurants: restaurants, menu: menu, orders: orders}
}

// Authenticate resolves a raw partner key to the restaurant that owns it. The
// key itself is never stored, only its hash, so this is a lookup by digest.
func (s *PartnerService) Authenticate(ctx context.Context, rawKey string) (domain.Restaurant, error) {
	return s.restaurants.GetByAPIKeyHash(ctx, apikey.Hash(rawKey))
}

func (s *PartnerService) GetProfile(ctx context.Context, restaurantID uuid.UUID) (domain.Restaurant, error) {
	return s.restaurants.GetByID(ctx, restaurantID)
}

func (s *PartnerService) SetAcceptingOrders(
	ctx context.Context, restaurantID uuid.UUID, accepting bool,
) (domain.Restaurant, error) {
	if err := s.restaurants.SetAcceptingOrders(ctx, restaurantID, accepting); err != nil {
		return domain.Restaurant{}, err
	}

	return s.restaurants.GetByID(ctx, restaurantID)
}

func (s *PartnerService) SyncMenu(
	ctx context.Context, restaurantID uuid.UUID, categories []domain.MenuSyncCategory,
) (domain.MenuSyncResult, error) {
	return s.menu.Sync(ctx, restaurantID, categories)
}

func (s *PartnerService) SetItemAvailability(
	ctx context.Context, restaurantID uuid.UUID, externalID string, available bool,
) error {
	return s.menu.SetItemAvailabilityByExternalID(ctx, restaurantID, externalID, available)
}

func (s *PartnerService) ListOrders(
	ctx context.Context, restaurantID uuid.UUID, status domain.OrderStatus, limit int,
) ([]domain.Order, error) {
	return s.orders.ListByRestaurantAndStatus(ctx, restaurantID, status, limit)
}

func (s *PartnerService) AcceptOrder(
	ctx context.Context, restaurantID, orderID uuid.UUID,
) (domain.Order, error) {
	return s.transition(ctx, restaurantID, orderID, domain.StatusAccepted, "")
}

func (s *PartnerService) RejectOrder(
	ctx context.Context, restaurantID, orderID uuid.UUID, reason string,
) (domain.Order, error) {
	return s.transition(ctx, restaurantID, orderID, domain.StatusRejected, reason)
}

func (s *PartnerService) AdvanceStatus(
	ctx context.Context, restaurantID, orderID uuid.UUID, to domain.OrderStatus,
) (domain.Order, error) {
	return s.transition(ctx, restaurantID, orderID, to, "")
}

func (s *PartnerService) transition(
	ctx context.Context, restaurantID, orderID uuid.UUID, to domain.OrderStatus, reason string,
) (domain.Order, error) {
	return s.orders.UpdateStatus(ctx, repository.UpdateStatusInput{
		OrderID:         orderID,
		To:              to,
		Actor:           domain.ActorRestaurant,
		RejectionReason: reason,
		RestaurantID:    &restaurantID,
	})
}
