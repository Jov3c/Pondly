package httpapi

import (
	"net/http"
	"time"

	"github.com/Jov3c/Pondly/apps/api/internal/store"
	"github.com/google/uuid"
)

func (a *App) ownerEnrollments(w http.ResponseWriter, r *http.Request, session store.OwnerSession) {
	key, ok := a.idempotencyKey(w, r)
	if !ok {
		return
	}
	var req enrollmentRequest
	body, ok := a.readJSON(w, r, &req)
	if !ok {
		return
	}
	result, err := a.store.CreateEnrollmentTicketWithProfile(r.Context(), session.OwnerID,
		store.EnrollmentProfile{DisplayName: req.DisplayName, AvatarKey: req.AvatarKey},
		"owner.enrollment.create", key, requestHash(body), a.cfg.EnrollmentTTL, a.cfg.MaxAgentsPerOwner)
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusCreated, result)
}

func (a *App) ownerAgents(w http.ResponseWriter, r *http.Request, session store.OwnerSession) {
	agents, err := a.store.ListOwnerAgents(r.Context(), session.OwnerID)
	if err != nil {
		failStore(w, r, err)
		return
	}
	if agents == nil {
		agents = []store.Agent{}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{
		"data": agents,
		"meta": map[string]any{"has_more": false},
	})
}

func (a *App) ownerClaim(w http.ResponseWriter, r *http.Request, session store.OwnerSession) {
	var req ownerClaimRequest
	body, ok := a.readJSON(w, r, &req)
	if !ok {
		return
	}
	key, ok := a.idempotencyKey(w, r)
	if !ok {
		return
	}
	agentID, err := uuid.Parse(req.AgentID)
	if err != nil || req.Challenge == "" {
		fail(w, r, http.StatusUnprocessableEntity, "invalid_input")
		return
	}
	result, err := a.store.ClaimAgent(r.Context(), session.OwnerID, agentID, req.Challenge,
		"owner.claim", key, requestHash(body))
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusOK, result)
}

func (a *App) ownerSuspend(w http.ResponseWriter, r *http.Request, session store.OwnerSession) {
	a.ownerAgentAction(w, r, session, "suspend")
}

func (a *App) ownerResume(w http.ResponseWriter, r *http.Request, session store.OwnerSession) {
	a.ownerAgentAction(w, r, session, "resume")
}

func (a *App) ownerRevoke(w http.ResponseWriter, r *http.Request, session store.OwnerSession) {
	a.ownerAgentAction(w, r, session, "revoke")
}

func (a *App) ownerAgentAction(w http.ResponseWriter, r *http.Request, session store.OwnerSession, action string) {
	agentID, ok := parseUUIDPath(w, r, "id")
	if !ok {
		return
	}
	key, ok := a.idempotencyKey(w, r)
	if !ok {
		return
	}
	agent, err := a.store.SetAgentStatus(r.Context(), session.OwnerID, agentID, action,
		"owner.agent."+action, key, emptyRequestHash())
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusOK, agent)
}

func (a *App) ownerDeactivate(w http.ResponseWriter, r *http.Request, session store.OwnerSession) {
	agentID, ok := parseUUIDPath(w, r, "id")
	if !ok {
		return
	}
	if _, ok := a.idempotencyKey(w, r); !ok {
		return
	}
	agent, err := a.store.DeactivateAgent(r.Context(), session.OwnerID, agentID)
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusOK, agent)
}

func (a *App) ownerPolicy(w http.ResponseWriter, r *http.Request, session store.OwnerSession) {
	agentID, ok := parseUUIDPath(w, r, "id")
	if !ok {
		return
	}
	policy, err := a.store.GetOwnerPolicy(r.Context(), session.OwnerID, agentID)
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusOK, policy)
}

func (a *App) updateOwnerPolicy(w http.ResponseWriter, r *http.Request, session store.OwnerSession) {
	agentID, ok := parseUUIDPath(w, r, "id")
	if !ok {
		return
	}
	var req ownerPolicyRequest
	if _, ok := a.readJSON(w, r, &req); !ok {
		return
	}
	if req.APIBudget == nil {
		fail(w, r, http.StatusUnprocessableEntity, "invalid_input")
		return
	}
	policy, err := a.store.UpdateOwnerPolicy(r.Context(), session.OwnerID, agentID, *req.APIBudget)
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusOK, policy)
}

func (a *App) ownerRecoveryTicket(w http.ResponseWriter, r *http.Request, session store.OwnerSession) {
	agentID, ok := parseUUIDPath(w, r, "id")
	if !ok {
		return
	}
	key, ok := a.idempotencyKey(w, r)
	if !ok {
		return
	}
	result, err := a.store.CreateRecoveryTicket(r.Context(), session.OwnerID, agentID,
		session.StepUpAt, "owner.agent.recovery_ticket", key, emptyRequestHash(),
		a.cfg.RecoveryTTL, 5*time.Minute)
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusCreated, result)
}
