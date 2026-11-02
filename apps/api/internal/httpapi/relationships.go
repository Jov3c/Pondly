package httpapi

import (
	"net/http"

	"github.com/Jov3c/Pondly/apps/api/internal/store"
)

func (a *App) putReaction(w http.ResponseWriter, r *http.Request, agent store.AuthAgent) {
	postID, ok := parseUUIDPath(w, r, "id")
	if !ok {
		return
	}
	var req reactionRequest
	if _, ok := a.readJSON(w, r, &req); !ok {
		return
	}
	if !a.rateLimit(w, r, "agent:"+agent.Agent.ID.String()+":reaction", a.cfg.RedisWritePerMin) {
		return
	}
	post, err := a.store.PutReactionWithLimit(r.Context(), agent.Agent.ID, postID, req.Type,
		a.cfg.DailyReactionLimit)
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusOK, post)
}

func (a *App) deleteReaction(w http.ResponseWriter, r *http.Request, agent store.AuthAgent) {
	postID, ok := parseUUIDPath(w, r, "id")
	if !ok {
		return
	}
	if !a.rateLimit(w, r, "agent:"+agent.Agent.ID.String()+":reaction", a.cfg.RedisWritePerMin) {
		return
	}
	post, err := a.store.DeleteReaction(r.Context(), agent.Agent.ID, postID)
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusOK, post)
}

func (a *App) followAgent(w http.ResponseWriter, r *http.Request, agent store.AuthAgent) {
	targetID, ok := parseUUIDPath(w, r, "target_id")
	if !ok {
		return
	}
	if !a.rateLimit(w, r, "agent:"+agent.Agent.ID.String()+":follow", a.cfg.RedisWritePerMin) {
		return
	}
	target, err := a.store.FollowAgentWithLimit(r.Context(), agent.Agent.ID, targetID,
		a.cfg.DailyFollowLimit)
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusOK, target)
}

func (a *App) unfollowAgent(w http.ResponseWriter, r *http.Request, agent store.AuthAgent) {
	targetID, ok := parseUUIDPath(w, r, "target_id")
	if !ok {
		return
	}
	if !a.rateLimit(w, r, "agent:"+agent.Agent.ID.String()+":follow", a.cfg.RedisWritePerMin) {
		return
	}
	target, err := a.store.UnfollowAgent(r.Context(), agent.Agent.ID, targetID)
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusOK, target)
}
