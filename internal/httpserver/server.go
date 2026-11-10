package httpserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"

	"github.com/veryshyjelly/bifrost/internal/delivery"
)

// Store contains only the diagnostic and reviewed-recovery operations needed by HTTP.
type Store interface {
	Counts(context.Context) (map[string]int64, error)
	Review(context.Context, int64, bool, bool) error
}

type Options struct {
	Ingress http.Handler
}

// Handler exposes diagnostics, manual review and optional durable provider ingress.
func Handler(store Store, admin string, options ...Options) http.Handler {
	mux := http.NewServeMux()
	var input http.Handler
	if len(options) > 0 {
		input = options[0].Ingress
	}
	write := func(w http.ResponseWriter, status int, value any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(value)
	}
	if input != nil {
		mux.Handle("/webhooks/", input)
		mux.Handle("/v1/management", input)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		write(w, 404, map[string]string{"error": "Not found"})
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		write(w, 200, map[string]any{"status": "ok", "configured": false, "ingress": input != nil})
	})
	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if len(admin) < 32 || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+admin)) != 1 {
				write(w, 401, map[string]string{"error": "Unauthorized"})
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc("GET /v1/status", auth(func(w http.ResponseWriter, r *http.Request) {
		counts, err := store.Counts(r.Context())
		if err != nil {
			write(w, 503, map[string]string{"error": "Database unavailable"})
			return
		}
		type row struct {
			State string `json:"state"`
			Count int64  `json:"count"`
		}
		rows := make([]row, 0, len(counts))
		for state, count := range counts {
			rows = append(rows, row{state, count})
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].State < rows[j].State })
		write(w, 200, map[string]any{"configured": false, "ingress": input != nil, "jobs": rows})
	}))
	mux.HandleFunc("POST /v1/review", auth(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
		var body struct {
			Job     int64  `json:"job"`
			Action  string `json:"action"`
			Confirm bool   `json:"confirmNoDelivery"`
		}
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&body); err != nil {
			var tooLarge *http.MaxBytesError
			status := 400
			if errors.As(err, &tooLarge) {
				status = 413
			}
			write(w, status, map[string]string{"error": "Invalid review JSON"})
			return
		}
		if decoder.Decode(new(any)) != io.EOF {
			write(w, 400, map[string]string{"error": "Expected one JSON object"})
			return
		}
		if body.Job <= 0 || (body.Action != "retry" && body.Action != "skip") {
			write(w, 400, map[string]string{"error": "Choose retry or skip for an existing failed/uncertain job"})
			return
		}
		err := store.Review(r.Context(), body.Job, body.Action == "retry", body.Confirm)
		switch {
		case errors.Is(err, delivery.ErrUncertain):
			write(w, 409, map[string]string{"error": "Inspect both chats and explicitly confirmNoDelivery before retrying"})
		case errors.Is(err, delivery.ErrInvalid):
			write(w, 400, map[string]string{"error": "Choose an existing failed/uncertain job"})
		case err != nil:
			write(w, 500, map[string]string{"error": "Internal error"})
		default:
			write(w, 200, map[string]bool{"accepted": true})
		}
	}))
	return mux
}
