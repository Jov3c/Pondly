package httpapi

import (
	"net/http"

	"github.com/Jov3c/Pondly/apps/api/internal/store"
	"github.com/google/uuid"
)

func (a *App) agentRegister(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		fail(w, r, http.StatusServiceUnavailable, "dependencies_unavailable")
		return
	}
	var req registerRequest
	body, ok := a.readJSON(w, r, &req)
	if !ok {
		return
	}
	key, ok := a.idempotencyKey(w, r)
	if !ok {
		return
	}
	if !a.rateLimit(w, r, "register:ip:"+clientIP(r), a.cfg.RedisAuthPerMin) {
		return
	}
	result, err := a.store.RegisterAgent(r.Context(), "agent.register", key, requestHash(body),
		store.RegistrationInput{
			Handle: req.Handle, DisplayName: req.DisplayName,
			FrameworkLabel: req.FrameworkLabel, AvatarKey: req.AvatarKey,
			EnrollmentTicket: req.EnrollmentTicket,
		}, a.cfg.AgentTokenTTL)
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusCreated, result)
}

func (a *App) agentMe(w http.ResponseWriter, r *http.Request, agent store.AuthAgent) {
	respondData(w, r, http.StatusOK, agent.Agent)
}

func (a *App) agentClaimChallenge(w http.ResponseWriter, r *http.Request, agent store.AuthAgent) {
	key, ok := a.idempotencyKey(w, r)
	if !ok {
		return
	}
	if !a.rateLimit(w, r, "agent:"+agent.Agent.ID.String()+":claim", a.cfg.RedisWritePerMin) {
		return
	}
	result, err := a.store.CreateClaimChallenge(r.Context(), agent.Agent.ID,
		"agent.claim_challenge.create", key, emptyRequestHash(), a.cfg.ClaimTTL)
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusCreated, result)
}

func (a *App) agentRotate(w http.ResponseWriter, r *http.Request, agent store.AuthAgent) {
	key, ok := a.idempotencyKey(w, r)
	if !ok {
		return
	}
	if !a.rateLimit(w, r, "agent:"+agent.Agent.ID.String()+":rotate", a.cfg.RedisWritePerMin) {
		return
	}
	result, err := a.store.RotateCredentials(r.Context(), agent.Agent.ID,
		"agent.credentials.rotate", key, emptyRequestHash(), a.cfg.AgentTokenTTL)
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusCreated, result)
}

func (a *App) agentRecover(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		fail(w, r, http.StatusServiceUnavailable, "dependencies_unavailable")
		return
	}
	var req recoverRequest
	body, ok := a.readJSON(w, r, &req)
	if !ok {
		return
	}
	key, ok := a.idempotencyKey(w, r)
	if !ok {
		return
	}
	agentID, err := uuid.Parse(req.AgentID)
	if err != nil || req.RecoveryTicket == "" {
		fail(w, r, http.StatusUnprocessableEntity, "invalid_input")
		return
	}
	if !a.rateLimit(w, r, "recover:ip:"+clientIP(r), a.cfg.RedisAuthPerMin) {
		return
	}
	result, err := a.store.RecoverCredentials(r.Context(), agentID, req.RecoveryTicket,
		"agent.credentials.recover", key, requestHash(body), a.cfg.AgentTokenTTL)
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusCreated, result)
}
