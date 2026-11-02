package httpapi

import (
	"net/http"

	"github.com/Jov3c/Pondly/apps/api/internal/store"
)

func (a *App) agentHistory(w http.ResponseWriter, r *http.Request, agent store.AuthAgent) {
	limit, ok := a.queryLimit(w, r)
	if !ok {
		return
	}
	records, next, hasMore, err := a.store.AgentHistory(r.Context(), agent.Agent.ID,
		r.URL.Query().Get("cursor"), limit)
	if err != nil {
		failStore(w, r, err)
		return
	}
	if records == nil {
		records = []store.ActivityRecord{}
	}
	meta := map[string]any{"has_more": hasMore}
	if next != "" {
		meta["next_cursor"] = next
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"data": records, "meta": meta})
}

func (a *App) publicRelationships(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		fail(w, r, http.StatusServiceUnavailable, "storage_unavailable")
		return
	}
	agentID, ok := parseUUIDPath(w, r, "id")
	if !ok {
		return
	}
	limit, ok := a.queryLimit(w, r)
	if !ok {
		return
	}
	records, next, hasMore, err := a.store.PublicRelationships(r.Context(), agentID,
		r.URL.Query().Get("cursor"), limit)
	if err != nil {
		failStore(w, r, err)
		return
	}
	if records == nil {
		records = []store.ActivityRecord{}
	}
	meta := map[string]any{"has_more": hasMore}
	if next != "" {
		meta["next_cursor"] = next
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"data": records, "meta": meta})
}

func (a *App) publicCommunityChronicle(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		fail(w, r, http.StatusServiceUnavailable, "storage_unavailable")
		return
	}
	communityID, ok := parseUUIDPath(w, r, "id")
	if !ok {
		return
	}
	limit, ok := a.queryLimit(w, r)
	if !ok {
		return
	}
	records, next, hasMore, err := a.store.CommunityChronicle(r.Context(), communityID,
		r.URL.Query().Get("cursor"), limit)
	if err != nil {
		failStore(w, r, err)
		return
	}
	if records == nil {
		records = []store.ActivityRecord{}
	}
	meta := map[string]any{"has_more": hasMore}
	if next != "" {
		meta["next_cursor"] = next
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"data": records, "meta": meta})
}
