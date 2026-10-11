package main

// events_routes.go — the admin view of the event sinks and the outbox, and the
// one write an operator needs: putting a dead event back in the queue.
//
// Admin only. A consumer sees nothing here: which sinks exist, and which events
// failed and why, is operational information.

import (
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

// eventSinkView is one configured sink with its delivery totals. The URL is
// left out on purpose: a webhook URL can carry a token in its query string.
type eventSinkView struct {
	Name          string   `json:"name"`
	Type          string   `json:"type"`
	Types         []string `json:"types"`
	Scenarios     []string `json:"scenarios"`
	Statuses      []string `json:"statuses"`
	IncludeParams bool     `json:"include_params"`
	MaxAttempts   int      `json:"max_attempts"`
	Pending       int      `json:"pending"`
	Sent          int      `json:"sent"`
	Dead          int      `json:"dead"`
	LastSentAt    *string  `json:"last_sent_at"`
}

var outboxStatuses = []string{"pending", "sent", "dead"}

// nonNilStrings keeps an empty filter as [] in JSON, not null, so a client
// need not treat the two differently.
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

const (
	outboxListDefault = 100
	outboxListMax     = 500
)

// registerEventRoutes wires the sink and outbox endpoints (admin only).
func registerEventRoutes(r chi.Router, deps routerDeps) {
	db := deps.db

	r.Get("/events/sinks", RequireRole(RoleAdmin, func(w http.ResponseWriter, _ *http.Request) {
		totals, err := db.outboxTotalsBySink()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out := make([]eventSinkView, 0, len(deps.eventSinks))
		for _, def := range deps.eventSinks {
			v := eventSinkView{
				Name:          def.Name,
				Type:          def.Type,
				Types:         nonNilStrings(def.Types),
				Scenarios:     nonNilStrings(def.Scenarios),
				Statuses:      nonNilStrings(def.Statuses),
				IncludeParams: def.IncludeParams,
				MaxAttempts:   def.MaxAttempts,
			}
			if t := totals[def.Name]; t != nil {
				v.Pending, v.Sent, v.Dead, v.LastSentAt = t.Pending, t.Sent, t.Dead, t.LastSentAt
			}
			out = append(out, v)
		}
		writeJSON(w, http.StatusOK, out)
	}))

	r.Get("/events/outbox", RequireRole(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		status := q.Get("status")
		if status != "" && !slices.Contains(outboxStatuses, status) {
			writeError(w, http.StatusBadRequest, "status must be one of pending, sent, dead")
			return
		}
		limit := outboxListDefault
		if s := q.Get("limit"); s != "" {
			n, err := strconv.Atoi(s)
			if err != nil || n < 1 || n > outboxListMax {
				writeError(w, http.StatusBadRequest, "limit must be between 1 and 500")
				return
			}
			limit = n
		}
		rows, err := db.listOutbox(status, q.Get("sink"), limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, rows)
	}))

	r.Post("/events/outbox/{id}/retry", RequireRole(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		exists, requeued, err := db.requeueDead(id, time.Now())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !exists {
			writeError(w, http.StatusNotFound, "event not found")
			return
		}
		if !requeued {
			writeError(w, http.StatusConflict, "only a dead event can be retried")
			return
		}
		// Who put an event back is part of the record: a retry is a decision
		// someone made, and a receiver may see the same event twice because of it.
		log.Info().Str("event_id", id).Str("by", principalName(PrincipalFrom(r.Context()))).
			Msg("job event requeued by operator")
		writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": "pending"})
	}))
}
