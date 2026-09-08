package partnerapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/dangyyyy/avito-kitchen/internal/platform/domain"
	"github.com/dangyyyy/avito-kitchen/internal/platform/service"
)

type contextKey struct{}

var restaurantContextKey contextKey

// Authenticator resolves X-Api-Key to a restaurant and puts it in the context.
// Handlers read the restaurant from there and never accept an identifier from
// the request, so one venue cannot address another venue's data.
func Authenticator(svc *service.PartnerService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("X-Api-Key")
			if key == "" {
				unauthorized(w, "missing X-Api-Key header")

				return
			}

			restaurant, err := svc.Authenticate(r.Context(), key)

			if errors.Is(err, domain.ErrNotFound) {
				unauthorized(w, "unknown api key")

				return
			}

			if err != nil {
				slog.ErrorContext(r.Context(), "partner authentication failed", "error", err)
				writeError(w, http.StatusInternalServerError, ErrorCodeInternalError, "internal error")

				return
			}

			ctx := context.WithValue(r.Context(), restaurantContextKey, restaurant)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func restaurantFrom(ctx context.Context) (domain.Restaurant, bool) {
	restaurant, ok := ctx.Value(restaurantContextKey).(domain.Restaurant)

	return restaurant, ok
}

func unauthorized(w http.ResponseWriter, message string) {
	writeError(w, http.StatusUnauthorized, ErrorCodeUnauthorized, message)
}

func writeError(w http.ResponseWriter, status int, code ErrorCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(Error{Code: code, Message: message}); err != nil {
		slog.Error("write partner error response", "error", err)
	}
}
