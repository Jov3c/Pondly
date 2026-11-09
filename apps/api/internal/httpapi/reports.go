package httpapi

import (
	"net/http"
	"strconv"

	"github.com/Jov3c/Pondly/apps/api/internal/store"
	"github.com/google/uuid"
)

func (a *App) createAgentReport(w http.ResponseWriter, r *http.Request, agent store.AuthAgent) {
	var req reportRequest
	body, ok := a.readJSON(w, r, &req)
	if !ok {
		return
	}
	key, ok := a.idempotencyKey(w, r)
	if !ok {
		return
	}
	targetID, err := uuid.Parse(req.TargetID)
	if err != nil {
		fail(w, r, http.StatusUnprocessableEntity, "invalid_input")
		return
	}
	if !a.rateLimit(w, r, "agent:"+agent.Agent.ID.String()+":report", a.cfg.RedisWritePerMin) {
		return
	}
	result, err := a.store.CreateAgentReport(r.Context(), agent.Agent.ID, req.TargetType,
		targetID, req.Reason, "agent.report.create", key, requestHash(body))
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusCreated, result)
}

func (a *App) createOwnerReport(w http.ResponseWriter, r *http.Request, session store.OwnerSession) {
	var req reportRequest
	body, ok := a.readJSON(w, r, &req)
	if !ok {
		return
	}
	key, ok := a.idempotencyKey(w, r)
	if !ok {
		return
	}
	targetID, err := uuid.Parse(req.TargetID)
	if err != nil {
		fail(w, r, http.StatusUnprocessableEntity, "invalid_input")
		return
	}
	if !a.rateLimit(w, r, "owner:"+session.OwnerID.String()+":report", a.cfg.RedisWritePerMin) {
		return
	}
	result, err := a.store.CreateOwnerReport(r.Context(), session.OwnerID, req.TargetType,
		targetID, req.Reason, "owner.report.create", key, requestHash(body))
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusCreated, result)
}

func (a *AdminApp) adminReports(w http.ResponseWriter, r *http.Request, _ store.AdminSession) {
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 50 {
			fail(w, r, http.StatusUnprocessableEntity, "invalid_input")
			return
		}
		limit = value
	}
	cases, next, hasMore, err := a.store.ListOpenReports(r.Context(), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		failStore(w, r, err)
		return
	}
	if cases == nil {
		cases = []store.ModerationCase{}
	}
	meta := map[string]any{"has_more": hasMore}
	if next != "" {
		meta["next_cursor"] = next
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"data": cases, "meta": meta})
}

func (a *AdminApp) adminSuspendAgent(w http.ResponseWriter, r *http.Request, session store.AdminSession) {
	agentID, ok := parseUUIDPath(w, r, "id")
	if !ok {
		return
	}
	var req moderationRequest
	body, ok := a.readJSON(w, r, &req)
	if !ok {
		return
	}
	key, ok := a.idempotencyKey(w, r)
	if !ok {
		return
	}
	if req.Decision != "suspend" || !validReason(req.Reason, store.MaxReasonRunes) {
		fail(w, r, http.StatusUnprocessableEntity, "invalid_input")
		return
	}
	agent, err := a.store.AdminSuspendAgent(r.Context(), session.AdminID, agentID, req.Reason,
		"admin.agent.suspend", key, requestHash(body))
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusOK, agent)
}

func (a *AdminApp) adminResolveReport(w http.ResponseWriter, r *http.Request, session store.AdminSession) {
	reportID, ok := parseUUIDPath(w, r, "id")
	if !ok {
		return
	}
	var req moderationRequest
	body, ok := a.readJSON(w, r, &req)
	if !ok {
		return
	}
	key, ok := a.idempotencyKey(w, r)
	if !ok {
		return
	}
	if (req.Decision != "resolve" && req.Decision != "dismiss") || !validReason(req.Reason, store.MaxReasonRunes) {
		fail(w, r, http.StatusUnprocessableEntity, "invalid_input")
		return
	}
	result, err := a.store.ResolveReport(r.Context(), session.AdminID, reportID, req.Decision,
		req.Reason, "admin.report.resolve", key, requestHash(body))
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusOK, result)
}
