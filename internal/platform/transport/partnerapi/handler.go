package partnerapi

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/dangyyyy/avito-kitchen/internal/platform/domain"
	"github.com/dangyyyy/avito-kitchen/internal/platform/service"
)

const defaultOrderLimit = 20

type Server struct {
	svc *service.PartnerService
}

func NewServer(svc *service.PartnerService) *Server {
	return &Server{svc: svc}
}

var _ StrictServerInterface = (*Server)(nil)

// errNoRestaurant means the auth middleware was not mounted in front of the
// handler. It is a wiring bug, not a client error, so it surfaces as a 500.
var errNoRestaurant = errors.New("restaurant missing from context")

func notFound(err error) Error {
	return Error{Code: ErrorCodeNotFound, Message: err.Error()}
}

func conflict(err error) Error {
	return Error{Code: ErrorCodeConflict, Message: err.Error()}
}

func toProfile(r domain.Restaurant) PartnerRestaurant {
	return PartnerRestaurant{Id: r.ID, Name: r.Name, IsAcceptingOrders: r.IsAcceptingOrders}
}

func optional(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

func toPartnerOrder(o domain.Order) PartnerOrder {
	items := make([]PartnerOrderItem, 0, len(o.Items))

	for _, item := range o.Items {
		var menuItemID uuid.UUID
		if item.MenuItemID != nil {
			menuItemID = *item.MenuItemID
		}

		items = append(items, PartnerOrderItem{
			MenuItemId:     menuItemID,
			ExternalItemId: item.ExternalItemID,
			Name:           item.Name,
			PriceCents:     item.PriceCents,
			Quantity:       item.Quantity,
		})
	}

	order := PartnerOrder{
		Id:              o.ID,
		Status:          PartnerOrderStatus(o.Status),
		TotalPriceCents: o.TotalPriceCents,
		DeliveryAddress: o.DeliveryAddress,
		CustomerPhone:   o.CustomerPhone,
		Comment:         optional(o.Comment),
		RejectionReason: optional(o.RejectionReason),
		Items:           items,
		CreatedAt:       o.CreatedAt,
	}

	if !o.UpdatedAt.IsZero() {
		order.UpdatedAt = &o.UpdatedAt
	}

	return order
}

func toSyncCategories(payload MenuSync) []domain.MenuSyncCategory {
	categories := make([]domain.MenuSyncCategory, 0, len(payload.Categories))

	for _, category := range payload.Categories {
		items := make([]domain.MenuSyncItem, 0, len(category.Items))

		for _, item := range category.Items {
			// Omitted availability means the item is on sale; a restaurant that
			// wants a stop list says so explicitly.
			available := true
			if item.IsAvailable != nil {
				available = *item.IsAvailable
			}

			description := ""
			if item.Description != nil {
				description = *item.Description
			}

			items = append(items, domain.MenuSyncItem{
				ExternalID:  item.ExternalId,
				Name:        item.Name,
				Description: description,
				PriceCents:  item.PriceCents,
				IsAvailable: available,
			})
		}

		position := 0
		if category.Position != nil {
			position = *category.Position
		}

		categories = append(categories, domain.MenuSyncCategory{
			ExternalID: category.ExternalId,
			Name:       category.Name,
			Position:   position,
			Items:      items,
		})
	}

	return categories
}

func (h *Server) GetProfile(ctx context.Context, _ GetProfileRequestObject) (GetProfileResponseObject, error) {
	restaurant, ok := restaurantFrom(ctx)
	if !ok {
		return nil, errNoRestaurant
	}

	return GetProfile200JSONResponse(toProfile(restaurant)), nil
}

func (h *Server) UpdateSettings(
	ctx context.Context, request UpdateSettingsRequestObject,
) (UpdateSettingsResponseObject, error) {
	restaurant, ok := restaurantFrom(ctx)
	if !ok {
		return nil, errNoRestaurant
	}

	if request.Body == nil {
		return UpdateSettings400JSONResponse{BadRequestJSONResponse(Error{
			Code: ErrorCodeBadRequest, Message: "body is required",
		})}, nil
	}

	updated, err := h.svc.SetAcceptingOrders(ctx, restaurant.ID, request.Body.IsAcceptingOrders)
	if err != nil {
		return nil, err
	}

	return UpdateSettings200JSONResponse(toProfile(updated)), nil
}

func (h *Server) SyncMenu(ctx context.Context, request SyncMenuRequestObject) (SyncMenuResponseObject, error) {
	restaurant, ok := restaurantFrom(ctx)
	if !ok {
		return nil, errNoRestaurant
	}

	if request.Body == nil {
		return SyncMenu400JSONResponse{BadRequestJSONResponse(Error{
			Code: ErrorCodeBadRequest, Message: "body is required",
		})}, nil
	}

	result, err := h.svc.SyncMenu(ctx, restaurant.ID, toSyncCategories(*request.Body))
	if err != nil {
		return nil, err
	}

	return SyncMenu200JSONResponse{
		Categories:       result.Categories,
		ItemsUpserted:    result.ItemsUpserted,
		ItemsDeactivated: result.ItemsDeactivated,
	}, nil
}

func (h *Server) SetItemAvailability(
	ctx context.Context, request SetItemAvailabilityRequestObject,
) (SetItemAvailabilityResponseObject, error) {
	restaurant, ok := restaurantFrom(ctx)
	if !ok {
		return nil, errNoRestaurant
	}

	if request.Body == nil {
		return SetItemAvailability400JSONResponse{BadRequestJSONResponse(Error{
			Code: ErrorCodeBadRequest, Message: "body is required",
		})}, nil
	}

	err := h.svc.SetItemAvailability(ctx, restaurant.ID, request.ExternalId, request.Body.IsAvailable)
	if errors.Is(err, domain.ErrNotFound) {
		return SetItemAvailability404JSONResponse{NotFoundJSONResponse(notFound(err))}, nil
	}

	if err != nil {
		return nil, err
	}

	return SetItemAvailability204Response{}, nil
}

func (h *Server) ListPartnerOrders(
	ctx context.Context, request ListPartnerOrdersRequestObject,
) (ListPartnerOrdersResponseObject, error) {
	restaurant, ok := restaurantFrom(ctx)
	if !ok {
		return nil, errNoRestaurant
	}

	limit := defaultOrderLimit
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}

	var status domain.OrderStatus
	if request.Params.Status != nil {
		status = domain.OrderStatus(*request.Params.Status)
	}

	orders, err := h.svc.ListOrders(ctx, restaurant.ID, status, limit)
	if err != nil {
		return nil, err
	}

	items := make([]PartnerOrder, 0, len(orders))
	for _, order := range orders {
		items = append(items, toPartnerOrder(order))
	}

	return ListPartnerOrders200JSONResponse{Items: items}, nil
}

func (h *Server) AcceptOrder(
	ctx context.Context, request AcceptOrderRequestObject,
) (AcceptOrderResponseObject, error) {
	restaurant, ok := restaurantFrom(ctx)
	if !ok {
		return nil, errNoRestaurant
	}

	order, err := h.svc.AcceptOrder(ctx, restaurant.ID, request.OrderId)

	if errors.Is(err, domain.ErrTransitionNotAllowed) {
		return AcceptOrder409JSONResponse{ConflictJSONResponse(conflict(err))}, nil
	}

	if errors.Is(err, domain.ErrNotFound) {
		return AcceptOrder404JSONResponse{NotFoundJSONResponse(notFound(err))}, nil
	}

	if err != nil {
		return nil, err
	}

	return AcceptOrder200JSONResponse{PartnerOrderResponseJSONResponse(toPartnerOrder(order))}, nil
}

func (h *Server) RejectOrder(
	ctx context.Context, request RejectOrderRequestObject,
) (RejectOrderResponseObject, error) {
	restaurant, ok := restaurantFrom(ctx)
	if !ok {
		return nil, errNoRestaurant
	}

	if request.Body == nil || request.Body.Reason == "" {
		return RejectOrder400JSONResponse{BadRequestJSONResponse(Error{
			Code: ErrorCodeBadRequest, Message: "reason is required",
		})}, nil
	}

	order, err := h.svc.RejectOrder(ctx, restaurant.ID, request.OrderId, request.Body.Reason)

	if errors.Is(err, domain.ErrTransitionNotAllowed) {
		return RejectOrder409JSONResponse{ConflictJSONResponse(conflict(err))}, nil
	}

	if errors.Is(err, domain.ErrNotFound) {
		return RejectOrder404JSONResponse{NotFoundJSONResponse(notFound(err))}, nil
	}

	if err != nil {
		return nil, err
	}

	return RejectOrder200JSONResponse{PartnerOrderResponseJSONResponse(toPartnerOrder(order))}, nil
}

func (h *Server) AdvanceOrderStatus(
	ctx context.Context, request AdvanceOrderStatusRequestObject,
) (AdvanceOrderStatusResponseObject, error) {
	restaurant, ok := restaurantFrom(ctx)
	if !ok {
		return nil, errNoRestaurant
	}

	if request.Body == nil {
		return AdvanceOrderStatus400JSONResponse{BadRequestJSONResponse(Error{
			Code: ErrorCodeBadRequest, Message: "body is required",
		})}, nil
	}

	target := domain.OrderStatus(request.Body.Status)
	if !target.Valid() {
		return AdvanceOrderStatus400JSONResponse{BadRequestJSONResponse(Error{
			Code:    ErrorCodeBadRequest,
			Message: fmt.Sprintf("unknown status %q", request.Body.Status),
		})}, nil
	}

	order, err := h.svc.AdvanceStatus(ctx, restaurant.ID, request.OrderId, target)

	if errors.Is(err, domain.ErrTransitionNotAllowed) {
		return AdvanceOrderStatus409JSONResponse{ConflictJSONResponse(conflict(err))}, nil
	}

	if errors.Is(err, domain.ErrNotFound) {
		return AdvanceOrderStatus404JSONResponse{NotFoundJSONResponse(notFound(err))}, nil
	}

	if err != nil {
		return nil, err
	}

	return AdvanceOrderStatus200JSONResponse{PartnerOrderResponseJSONResponse(toPartnerOrder(order))}, nil
}
