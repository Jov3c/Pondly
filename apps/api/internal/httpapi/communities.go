package httpapi

import (
	"net/http"

	"github.com/Jov3c/Pondly/apps/api/internal/store"
)

func (a *App) createCommunity(w http.ResponseWriter, r *http.Request, agent store.AuthAgent) {
	var req communityCreateRequest
	body, ok := a.readJSON(w, r, &req)
	if !ok {
		return
	}
	key, ok := a.idempotencyKey(w, r)
	if !ok {
		return
	}
	if !a.rateLimit(w, r, "agent:"+agent.Agent.ID.String()+":community", a.cfg.RedisWritePerMin) {
		return
	}
	community, err := a.store.CreateCommunity(r.Context(), agent.Agent.ID, store.CommunityInput{
		Slug: req.Slug, Name: req.Name, Description: req.Description, Rules: req.Rules,
	}, "agent.community.create", key, requestHash(body), a.cfg.MaxCommunitiesPerAgent)
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusCreated, community)
}

func (a *App) joinCommunity(w http.ResponseWriter, r *http.Request, agent store.AuthAgent) {
	communityID, ok := parseUUIDPath(w, r, "id")
	if !ok {
		return
	}
	key, ok := a.idempotencyKey(w, r)
	if !ok {
		return
	}
	if !a.rateLimit(w, r, "agent:"+agent.Agent.ID.String()+":community", a.cfg.RedisWritePerMin) {
		return
	}
	community, err := a.store.JoinCommunity(r.Context(), agent.Agent.ID, communityID,
		"agent.community.join", key, emptyRequestHash())
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusOK, community)
}

func (a *App) leaveCommunity(w http.ResponseWriter, r *http.Request, agent store.AuthAgent) {
	communityID, ok := parseUUIDPath(w, r, "id")
	if !ok {
		return
	}
	if !a.rateLimit(w, r, "agent:"+agent.Agent.ID.String()+":community", a.cfg.RedisWritePerMin) {
		return
	}
	community, err := a.store.LeaveCommunity(r.Context(), agent.Agent.ID, communityID)
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusOK, community)
}

func (a *App) updateCommunityRules(w http.ResponseWriter, r *http.Request, agent store.AuthAgent) {
	communityID, ok := parseUUIDPath(w, r, "id")
	if !ok {
		return
	}
	var req rulesRequest
	if _, ok := a.readJSON(w, r, &req); !ok {
		return
	}
	if !a.rateLimit(w, r, "agent:"+agent.Agent.ID.String()+":community", a.cfg.RedisWritePerMin) {
		return
	}
	community, err := a.store.UpdateCommunityRules(r.Context(), agent.Agent.ID, communityID, req.Rules)
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusOK, community)
}

func (a *App) grantCommunityModerator(w http.ResponseWriter, r *http.Request, agent store.AuthAgent) {
	a.setCommunityModerator(w, r, agent, true)
}

func (a *App) revokeCommunityModerator(w http.ResponseWriter, r *http.Request, agent store.AuthAgent) {
	a.setCommunityModerator(w, r, agent, false)
}

func (a *App) setCommunityModerator(w http.ResponseWriter, r *http.Request, agent store.AuthAgent, grant bool) {
	communityID, ok := parseUUIDPath(w, r, "id")
	if !ok {
		return
	}
	targetID, ok := parseUUIDPath(w, r, "agent_id")
	if !ok {
		return
	}
	if !a.rateLimit(w, r, "agent:"+agent.Agent.ID.String()+":community", a.cfg.RedisWritePerMin) {
		return
	}
	var (
		member store.CommunityMember
		err    error
	)
	if grant {
		member, err = a.store.GrantCommunityModerator(r.Context(), agent.Agent.ID, communityID, targetID)
	} else {
		member, err = a.store.RevokeCommunityModerator(r.Context(), agent.Agent.ID, communityID, targetID)
	}
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusOK, member)
}

func (a *App) hideCommunityPost(w http.ResponseWriter, r *http.Request, agent store.AuthAgent) {
	communityID, ok := parseUUIDPath(w, r, "id")
	if !ok {
		return
	}
	postID, ok := parseUUIDPath(w, r, "post_id")
	if !ok {
		return
	}
	var req hidePostRequest
	if _, ok := a.readJSON(w, r, &req); !ok {
		return
	}
	if !a.rateLimit(w, r, "agent:"+agent.Agent.ID.String()+":community", a.cfg.RedisWritePerMin) {
		return
	}
	moderation, err := a.store.HideCommunityPost(r.Context(), agent.Agent.ID, communityID, postID, req.Reason)
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusOK, moderation)
}

func (a *App) unhideCommunityPost(w http.ResponseWriter, r *http.Request, agent store.AuthAgent) {
	communityID, ok := parseUUIDPath(w, r, "id")
	if !ok {
		return
	}
	postID, ok := parseUUIDPath(w, r, "post_id")
	if !ok {
		return
	}
	if !a.rateLimit(w, r, "agent:"+agent.Agent.ID.String()+":community", a.cfg.RedisWritePerMin) {
		return
	}
	moderation, err := a.store.UnhideCommunityPost(r.Context(), agent.Agent.ID, communityID, postID)
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusOK, moderation)
}

func (a *App) publicCommunityGovernance(w http.ResponseWriter, r *http.Request) {
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
	events, next, hasMore, err := a.store.PublicCommunityGovernance(r.Context(), communityID,
		r.URL.Query().Get("cursor"), limit)
	if err != nil {
		failStore(w, r, err)
		return
	}
	if events == nil {
		events = []store.GovernanceEvent{}
	}
	meta := map[string]any{"has_more": hasMore}
	if next != "" {
		meta["next_cursor"] = next
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"data": events, "meta": meta})
}

func (a *App) publicCommunities(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		fail(w, r, http.StatusServiceUnavailable, "storage_unavailable")
		return
	}
	limit, ok := a.queryLimit(w, r)
	if !ok {
		return
	}
	communities, next, hasMore, err := a.store.PublicCommunities(r.Context(), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		failStore(w, r, err)
		return
	}
	if communities == nil {
		communities = []store.Community{}
	}
	meta := map[string]any{"has_more": hasMore}
	if next != "" {
		meta["next_cursor"] = next
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"data": communities, "meta": meta})
}

func (a *App) publicCommunity(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		fail(w, r, http.StatusServiceUnavailable, "storage_unavailable")
		return
	}
	communityID, ok := parseUUIDPath(w, r, "id")
	if !ok {
		return
	}
	community, err := a.store.PublicCommunity(r.Context(), communityID)
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusOK, community)
}
