package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dangyyyy/avito-kitchen/internal/platform/repository"
	"github.com/dangyyyy/avito-kitchen/internal/platform/service"
	"github.com/dangyyyy/avito-kitchen/internal/platform/transport/clientapi"
	"github.com/dangyyyy/avito-kitchen/internal/platform/transport/partnerapi"
)

func newRouter(pool *pgxpool.Pool) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer)

	r.Get("/healthz", healthz(pool))

	clientService := service.NewClientService(
		repository.NewRestaurantRepository(pool),
		repository.NewMenuRepository(pool),
		repository.NewOrderRepository(pool),
		repository.NewUserRepository(pool),
	)

	strict := clientapi.NewStrictHandlerWithOptions(
		clientapi.NewServer(clientService),
		nil,
		clientapi.StrictHTTPServerOptions{
			RequestErrorHandlerFunc:  writeRequestError,
			ResponseErrorHandlerFunc: writeInternalError,
		},
	)

	// ErrorHandlerFunc has to be set here as well, not only on the strict handler:
	// parameter decoding fails inside the chi wrapper, before the strict layer is
	// reached. Without it a malformed UUID answers with plain text instead of the
	// error envelope the spec promises.
	clientapi.HandlerWithOptions(strict, clientapi.ChiServerOptions{
		BaseRouter:       r,
		ErrorHandlerFunc: writeRequestError,
	})

	partnerService := service.NewPartnerService(
		repository.NewRestaurantRepository(pool),
		repository.NewMenuRepository(pool),
		repository.NewOrderRepository(pool),
	)

	partnerStrict := partnerapi.NewStrictHandlerWithOptions(
		partnerapi.NewServer(partnerService),
		nil,
		partnerapi.StrictHTTPServerOptions{
			RequestErrorHandlerFunc:  writePartnerRequestError,
			ResponseErrorHandlerFunc: writeInternalError,
		},
	)

	// The authenticator is registered as a middleware here rather than inside the
	// handlers, so every partner route is covered by construction.
	partnerapi.HandlerWithOptions(partnerStrict, partnerapi.ChiServerOptions{
		BaseRouter:       r,
		Middlewares:      []partnerapi.MiddlewareFunc{partnerapi.Authenticator(partnerService)},
		ErrorHandlerFunc: writePartnerRequestError,
	})

	return r
}

func writePartnerRequestError(w http.ResponseWriter, _ *http.Request, err error) {
	writeJSON(w, http.StatusBadRequest, partnerapi.Error{
		Code:    partnerapi.ErrorCodeBadRequest,
		Message: err.Error(),
	})
}

// writeRequestError answers malformed requests — a bad UUID in the path, a
// missing required header, a body that is not valid JSON.
func writeRequestError(w http.ResponseWriter, _ *http.Request, err error) {
	writeJSON(w, http.StatusBadRequest, clientapi.Error{
		Code:    clientapi.ErrorCodeBadRequest,
		Message: err.Error(),
	})
}

// writeInternalError keeps the internal message in the log and returns a generic
// body, so a database error never reaches the client verbatim.
func writeInternalError(w http.ResponseWriter, r *http.Request, err error) {
	slog.ErrorContext(r.Context(), "request failed",
		"method", r.Method, "path", r.URL.Path, "error", err)

	writeJSON(w, http.StatusInternalServerError, clientapi.Error{
		Code:    clientapi.ErrorCodeInternalError,
		Message: "internal error",
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Error("write response", "error", err)
	}
}

func healthz(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := pool.Ping(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "database unavailable"})

			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
