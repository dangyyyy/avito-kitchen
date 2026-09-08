package clientapi

import (
	"context"
	"errors"

	"github.com/dangyyyy/avito-kitchen/internal/platform/domain"
	"github.com/dangyyyy/avito-kitchen/internal/platform/repository"
	"github.com/dangyyyy/avito-kitchen/internal/platform/service"
)

type Server struct {
	svc *service.ClientService
}

func NewServer(svc *service.ClientService) *Server {
	return &Server{svc: svc}
}

// Compile-time proof that the handler still matches the generated contract.
// If the spec grows an operation, this line stops building.
var _ StrictServerInterface = (*Server)(nil)

func notFound(err error) Error {
	return Error{Code: ErrorCodeNotFound, Message: err.Error()}
}

func badRequest(err error) Error {
	return Error{Code: ErrorCodeBadRequest, Message: err.Error()}
}

// cartConflict turns a rejected checkout into the 409 body. A closed restaurant
// and an unusable cart are the same class of problem for the client: nothing is
// wrong with the request, the world changed underneath it.
func cartConflict(err error) (CartConflict, bool) {
	var cartErr *domain.CartValidationError
	if errors.As(err, &cartErr) {
		return CartConflict{
			Code:     CartConflictCodeCartInvalid,
			Message:  "some items are no longer orderable",
			Problems: toItemProblems(cartErr.Problems),
		}, true
	}

	if errors.Is(err, domain.ErrRestaurantClosed) {
		return CartConflict{
			Code:     CartConflictCodeRestaurantClosed,
			Message:  "restaurant is not accepting orders",
			Problems: []ItemProblem{},
		}, true
	}

	return CartConflict{}, false
}

func (h *Server) ListRestaurants(
	ctx context.Context, request ListRestaurantsRequestObject,
) (ListRestaurantsResponseObject, error) {
	limit, offset := pageOrDefault(request.Params.Limit, request.Params.Offset)

	filter := domain.RestaurantFilter{Limit: limit, Offset: offset}
	if request.Params.City != nil {
		filter.City = *request.Params.City
	}

	if request.Params.Cuisine != nil {
		filter.Cuisine = *request.Params.Cuisine
	}

	if request.Params.OpenOnly != nil {
		filter.OpenOnly = *request.Params.OpenOnly
	}

	restaurants, err := h.svc.ListRestaurants(ctx, filter)
	if err != nil {
		return nil, err
	}

	return ListRestaurants200JSONResponse{Items: toRestaurants(restaurants)}, nil
}

func (h *Server) GetRestaurant(
	ctx context.Context, request GetRestaurantRequestObject,
) (GetRestaurantResponseObject, error) {
	restaurant, err := h.svc.GetRestaurant(ctx, request.RestaurantId)
	if errors.Is(err, domain.ErrNotFound) {
		return GetRestaurant404JSONResponse{NotFoundJSONResponse(notFound(err))}, nil
	}

	if err != nil {
		return nil, err
	}

	return GetRestaurant200JSONResponse(toRestaurant(restaurant)), nil
}

func (h *Server) GetMenu(ctx context.Context, request GetMenuRequestObject) (GetMenuResponseObject, error) {
	categories, err := h.svc.GetMenu(ctx, request.RestaurantId)
	if errors.Is(err, domain.ErrNotFound) {
		return GetMenu404JSONResponse{NotFoundJSONResponse(notFound(err))}, nil
	}

	if err != nil {
		return nil, err
	}

	return GetMenu200JSONResponse(toMenu(request.RestaurantId, categories)), nil
}

func (h *Server) SearchMenuItems(
	ctx context.Context, request SearchMenuItemsRequestObject,
) (SearchMenuItemsResponseObject, error) {
	limit, offset := pageOrDefault(request.Params.Limit, request.Params.Offset)

	results, err := h.svc.SearchDishes(ctx, request.Params.Query, limit, offset)
	if err != nil {
		return nil, err
	}

	return SearchMenuItems200JSONResponse{Items: toSearchHits(results)}, nil
}

func (h *Server) ValidateCart(
	ctx context.Context, request ValidateCartRequestObject,
) (ValidateCartResponseObject, error) {
	if request.Body == nil || len(request.Body.Items) == 0 {
		return ValidateCart400JSONResponse{
			BadRequestJSONResponse(badRequest(domain.ErrEmptyOrder)),
		}, nil
	}

	result, err := h.svc.ValidateCart(ctx, repository.PlaceOrderInput{
		RestaurantID: request.Body.RestaurantId,
		Items:        toRequestedItems(request.Body.Items),
	})

	if errors.Is(err, domain.ErrNotFound) {
		return ValidateCart404JSONResponse{NotFoundJSONResponse(notFound(err))}, nil
	}

	// A closed restaurant is reported as an invalid cart rather than an error:
	// the caller asked a question and gets an answer.
	if errors.Is(err, domain.ErrRestaurantClosed) {
		return ValidateCart200JSONResponse{Valid: false, Problems: []ItemProblem{}}, nil
	}

	if err != nil {
		return nil, err
	}

	return ValidateCart200JSONResponse{
		Valid:           result.Valid,
		TotalPriceCents: result.TotalPriceCents,
		Problems:        toItemProblems(result.Problems),
	}, nil
}

func (h *Server) PlaceOrder(
	ctx context.Context, request PlaceOrderRequestObject,
) (PlaceOrderResponseObject, error) {
	if request.Body == nil || len(request.Body.Items) == 0 {
		return PlaceOrder400JSONResponse{
			BadRequestJSONResponse(badRequest(domain.ErrEmptyOrder)),
		}, nil
	}

	order, err := h.svc.PlaceOrder(ctx, repository.PlaceOrderInput{
		RestaurantID:    request.Body.RestaurantId,
		UserID:          request.Params.XUserId,
		DeliveryAddress: request.Body.DeliveryAddress,
		CustomerPhone:   request.Body.CustomerPhone,
		Comment:         valueOrEmpty(request.Body.Comment),
		IdempotencyKey:  request.Params.XIdempotencyKey,
		Items:           toRequestedItems(request.Body.Items),
	})

	if conflict, ok := cartConflict(err); ok {
		return PlaceOrder409JSONResponse(conflict), nil
	}

	if errors.Is(err, domain.ErrNotFound) {
		return PlaceOrder404JSONResponse{NotFoundJSONResponse(notFound(err))}, nil
	}

	if err != nil {
		return nil, err
	}

	return PlaceOrder201JSONResponse(toOrder(order)), nil
}

func (h *Server) ListOrders(
	ctx context.Context, request ListOrdersRequestObject,
) (ListOrdersResponseObject, error) {
	limit, offset := pageOrDefault(request.Params.Limit, request.Params.Offset)

	orders, err := h.svc.ListOrders(ctx, request.Params.XUserId, limit, offset)
	if err != nil {
		return nil, err
	}

	return ListOrders200JSONResponse{Items: toOrders(orders)}, nil
}

func (h *Server) GetOrder(ctx context.Context, request GetOrderRequestObject) (GetOrderResponseObject, error) {
	order, err := h.svc.GetOrder(ctx, request.Params.XUserId, request.OrderId)
	if errors.Is(err, domain.ErrNotFound) {
		return GetOrder404JSONResponse{NotFoundJSONResponse(notFound(err))}, nil
	}

	if err != nil {
		return nil, err
	}

	return GetOrder200JSONResponse(toOrder(order)), nil
}

func (h *Server) CancelOrder(
	ctx context.Context, request CancelOrderRequestObject,
) (CancelOrderResponseObject, error) {
	order, err := h.svc.CancelOrder(ctx, request.Params.XUserId, request.OrderId)

	if errors.Is(err, domain.ErrTransitionNotAllowed) {
		return CancelOrder409JSONResponse(Error{
			Code:    ErrorCodeConflict,
			Message: err.Error(),
		}), nil
	}

	if errors.Is(err, domain.ErrNotFound) {
		return CancelOrder404JSONResponse{NotFoundJSONResponse(notFound(err))}, nil
	}

	if err != nil {
		return nil, err
	}

	return CancelOrder200JSONResponse(toOrder(order)), nil
}

func valueOrEmpty(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}
