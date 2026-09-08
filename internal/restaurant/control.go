package restaurant

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewControlRouter exposes the levers a demo needs: send an item to the stop
// list, close the venue, refuse the next order. Without them the interesting
// failure paths could only be triggered by waiting for chance.
func NewControlRouter(kitchen *Kitchen) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Post("/control/stop-list", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ExternalID  string `json:"external_id"`
			IsAvailable bool   `json:"is_available"`
		}

		if !decode(w, r, &body) {
			return
		}

		if body.ExternalID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "external_id is required"})

			return
		}

		err := kitchen.SetItemAvailability(r.Context(), body.ExternalID, body.IsAvailable)
		if errors.Is(err, ErrItemNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "menu item not found"})

			return
		}

		if err != nil {
			slog.ErrorContext(r.Context(), "stop list", "error", err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "platform call failed"})

			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"external_id": body.ExternalID, "is_available": body.IsAvailable,
		})
	})

	r.Post("/control/accepting-orders", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IsAcceptingOrders bool `json:"is_accepting_orders"`
		}

		if !decode(w, r, &body) {
			return
		}

		if err := kitchen.SetAcceptingOrders(r.Context(), body.IsAcceptingOrders); err != nil {
			slog.ErrorContext(r.Context(), "accepting orders", "error", err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "platform call failed"})

			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"is_accepting_orders": body.IsAcceptingOrders})
	})

	r.Post("/control/pause", func(w http.ResponseWriter, _ *http.Request) {
		kitchen.SetPaused(true)
		writeJSON(w, http.StatusOK, map[string]bool{"paused": true})
	})

	r.Post("/control/resume", func(w http.ResponseWriter, _ *http.Request) {
		kitchen.SetPaused(false)
		writeJSON(w, http.StatusOK, map[string]bool{"paused": false})
	})

	r.Post("/control/reject-next", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Reason string `json:"reason"`
		}

		if !decode(w, r, &body) {
			return
		}

		if body.Reason == "" {
			body.Reason = "заведение не может принять заказ"
		}

		kitchen.ArmRejection(body.Reason)
		writeJSON(w, http.StatusOK, map[string]string{"armed_reason": body.Reason})
	})

	return r
}

func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})

		return false
	}

	return true
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Error("write control response", "error", err)
	}
}
