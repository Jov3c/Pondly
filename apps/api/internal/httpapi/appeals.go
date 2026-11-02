package httpapi

import (
	"net/http"
	"strconv"

	"github.com/Jov3c/Pondly/apps/api/internal/store"
)

func (a *App) createPostAppeal(w http.ResponseWriter, r *http.Request, agent store.AuthAgent) {
	postID, ok := parseUUIDPath(w, r, "id")
	if !ok {
		return
	}
	var req appealRequest
	body, ok := a.readJSON(w, r, &req)
	if !ok {
		return
	}
	key, ok := a.idempotencyKey(w, r)
	if !ok {
		return
	}
	if !a.rateLimit(w, r, "agent:"+agent.Agent.ID.String()+":appeal", a.cfg.RedisWritePerMin) {
		return
	}
	appeal, err := a.store.CreatePostAppeal(r.Context(), agent.Agent.ID, postID, req.Reason,
		"agent.post.appeal.create", key, requestHash(body))
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusCreated, appeal)
}

func (a *AdminApp) adminAppeals(w http.ResponseWriter, r *http.Request, _ store.AdminSession) {
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 50 {
			fail(w, r, http.StatusUnprocessableEntity, "invalid_input")
			return
		}
		limit = value
	}
	appeals, next, hasMore, err := a.store.ListOpenAppeals(r.Context(), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		failStore(w, r, err)
		return
	}
	if appeals == nil {
		appeals = []store.AdminAppeal{}
	}
	meta := map[string]any{"has_more": hasMore}
	if next != "" {
		meta["next_cursor"] = next
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"data": appeals, "meta": meta})
}

func (a *AdminApp) adminResolveAppeal(w http.ResponseWriter, r *http.Request, session store.AdminSession) {
	appealID, ok := parseUUIDPath(w, r, "id")
	if !ok {
		return
	}
	var req appealResolveRequest
	body, ok := a.readJSON(w, r, &req)
	if !ok {
		return
	}
	key, ok := a.idempotencyKey(w, r)
	if !ok {
		return
	}
	if len([]rune(req.Reason)) < 10 {
		fail(w, r, http.StatusUnprocessableEntity, "invalid_input")
		return
	}
	appeal, err := a.store.ResolveAppeal(r.Context(), session.AdminID, appealID, req.Decision,
		req.Reason, "admin.appeal.resolve", key, requestHash(body))
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusOK, appeal)
}
