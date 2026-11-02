package httpapi

import (
	"net/http"

	"github.com/Jov3c/Pondly/apps/api/internal/auth"
	"github.com/Jov3c/Pondly/apps/api/internal/store"
)

func (a *App) agentEvents(w http.ResponseWriter, r *http.Request, agent store.AuthAgent) {
	limit, ok := a.queryLimit(w, r)
	if !ok {
		return
	}
	cursor := r.URL.Query().Get("cursor")
	afterSeq := int64(0)
	if cursor != "" {
		cursorAgent, seq, err := auth.DecodeCursor(a.cfg.CursorKey, cursor)
		if err != nil || cursorAgent != agent.Agent.ID {
			fail(w, r, http.StatusBadRequest, "invalid_cursor")
			return
		}
		afterSeq = seq
	}
	page, err := a.store.GetAgentEvents(r.Context(), agent.Agent.ID, afterSeq, limit)
	if err != nil {
		if err == store.ErrInvalidCursor {
			fail(w, r, http.StatusBadRequest, "invalid_cursor")
			return
		}
		failStore(w, r, err)
		return
	}
	if page.CursorGap {
		restart, err := auth.EncodeCursor(a.cfg.CursorKey, agent.Agent.ID, page.LowestRetained-1)
		if err != nil {
			fail(w, r, http.StatusServiceUnavailable, "dependencies_unavailable")
			return
		}
		respondJSON(w, r, http.StatusGone, map[string]any{
			"error": map[string]any{
				"code":       "cursor_expired",
				"message":    http.StatusText(http.StatusGone),
				"request_id": requestIDFrom(r),
				"details": map[string]any{
					"restart_cursor":     restart,
					"retention_days":     store.NotificationRetentionDays,
					"history_incomplete": true,
				},
			},
		})
		return
	}
	events := page.Events
	if events == nil {
		events = []store.Event{}
	}
	next, err := auth.EncodeCursor(a.cfg.CursorKey, agent.Agent.ID, page.NextSeq)
	if err != nil {
		fail(w, r, http.StatusServiceUnavailable, "dependencies_unavailable")
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{
		"data": events,
		"meta": map[string]any{"has_more": page.HasMore, "next_cursor": next},
	})
}
