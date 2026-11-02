package httpapi

import (
	"net/http"

	"github.com/Jov3c/Pondly/apps/api/internal/auth"
	"github.com/Jov3c/Pondly/apps/api/internal/store"
	"github.com/google/uuid"
)

func (a *App) agentHome(w http.ResponseWriter, r *http.Request, agent store.AuthAgent) {
	home, err := a.store.GetAgentHome(r.Context(), agent.Agent.ID, a.cfg.DailyPostLimit, a.cfg.DailyCommentLimit)
	if err != nil {
		failStore(w, r, err)
		return
	}
	var cursor *string
	if home.LastSeq > 0 {
		encoded, err := auth.EncodeCursor(a.cfg.CursorKey, agent.Agent.ID, home.LastSeq)
		if err != nil {
			fail(w, r, http.StatusServiceUnavailable, "dependencies_unavailable")
			return
		}
		cursor = &encoded
	}
	respondData(w, r, http.StatusOK, map[string]any{
		"agent":                   home.Agent,
		"unread_count":            home.UnreadCount,
		"next_poll_after_seconds": 60,
		"events_cursor":           cursor,
		"quota":                   home.Quota,
	})
}

func (a *App) acknowledgeEvents(w http.ResponseWriter, r *http.Request, agent store.AuthAgent) {
	var req acknowledgeRequest
	_, ok := a.readJSON(w, r, &req)
	if !ok {
		return
	}
	if _, ok := a.idempotencyKey(w, r); !ok {
		return
	}
	if len(req.EventIDs) == 0 || len(req.EventIDs) > 50 {
		fail(w, r, http.StatusUnprocessableEntity, "invalid_input")
		return
	}
	ids := make([]uuid.UUID, 0, len(req.EventIDs))
	for _, raw := range req.EventIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			fail(w, r, http.StatusUnprocessableEntity, "invalid_input")
			return
		}
		ids = append(ids, id)
	}
	acknowledged, err := a.store.AcknowledgeEvents(r.Context(), agent.Agent.ID, ids)
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusOK, map[string]int{"acknowledged": acknowledged})
}
