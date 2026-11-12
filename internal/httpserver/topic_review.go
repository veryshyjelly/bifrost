package httpserver

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/recovery"
)

func topicReviewRoutes(mux *http.ServeMux, topics recovery.Topics, auth func(http.HandlerFunc) http.HandlerFunc, write func(http.ResponseWriter, int, any)) {
	failure := func(w http.ResponseWriter, err error) {
		switch {
		case errors.Is(err, recovery.ErrConflict):
			write(w, 409, map[string]string{"error": "Topic review changed or remains blocked; inspect the job again"})
		case errors.Is(err, delivery.ErrInvalid):
			write(w, 400, map[string]string{"error": "Choose an owned topic creation with matching explicit review evidence"})
		default:
			write(w, 503, map[string]string{"error": "Topic review unavailable"})
		}
	}
	mux.HandleFunc("GET /v1/topic-review", auth(func(w http.ResponseWriter, r *http.Request) {
		ids := r.URL.Query()["job"]
		if len(ids) != 1 {
			write(w, 400, map[string]string{"error": "Choose one positive job ID"})
			return
		}
		id, err := strconv.ParseInt(ids[0], 10, 64)
		if err != nil || id <= 0 {
			write(w, 400, map[string]string{"error": "Choose one positive job ID"})
			return
		}
		result, err := topics.InspectTopic(r.Context(), id)
		if err != nil {
			failure(w, err)
			return
		}
		write(w, 200, result)
	}))
	mux.HandleFunc("POST /v1/topic-review", auth(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		var decision recovery.TopicDecision
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&decision); err != nil {
			var large *http.MaxBytesError
			if errors.As(err, &large) {
				write(w, 413, map[string]string{"error": "Topic review JSON too large"})
			} else {
				write(w, 400, map[string]string{"error": "Invalid topic review JSON"})
			}
			return
		}
		if decoder.Decode(new(any)) != io.EOF || !decision.Valid() {
			write(w, 400, map[string]string{"error": "Supply one complete, explicitly confirmed topic decision"})
			return
		}
		result, err := topics.ReconcileTopic(r.Context(), decision)
		if err != nil {
			failure(w, err)
			return
		}
		write(w, 200, result)
	}))
}
