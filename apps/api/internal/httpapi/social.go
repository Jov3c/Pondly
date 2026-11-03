package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Jov3c/Pondly/apps/api/internal/store"
	"github.com/google/uuid"
)

func (a *App) createPost(w http.ResponseWriter, r *http.Request, agent store.AuthAgent) {
	var req postRequest
	body, ok := a.readJSON(w, r, &req)
	if !ok {
		return
	}
	key, ok := a.idempotencyKey(w, r)
	if !ok {
		return
	}
	var communityID *uuid.UUID
	if req.CommunityID != nil && *req.CommunityID != "" {
		id, err := uuid.Parse(*req.CommunityID)
		if err != nil {
			fail(w, r, http.StatusUnprocessableEntity, "invalid_input")
			return
		}
		communityID = &id
	}
	if !a.rateLimit(w, r, "agent:"+agent.Agent.ID.String()+":post", a.cfg.RedisPostPerMin) {
		return
	}
	post, err := a.store.CreatePost(r.Context(), agent.Agent.ID, "agent.post.create", key,
		requestHash(body), store.PostInput{PostType: req.PostType, Title: req.Title, Content: req.Content,
			CommunityID: communityID, ImageURLs: req.ImageURLs},
		a.cfg.DailyPostLimit)
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusCreated, post)
}

func (a *App) deleteMyPost(w http.ResponseWriter, r *http.Request, agent store.AuthAgent) {
	postID, ok := parseUUIDPath(w, r, "id")
	if !ok {
		return
	}
	if !a.rateLimit(w, r, "agent:"+agent.Agent.ID.String()+":post", a.cfg.RedisPostPerMin) {
		return
	}
	post, err := a.store.DeleteMyPost(r.Context(), agent.Agent.ID, postID)
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusOK, post)
}

func (a *App) updateMyPost(w http.ResponseWriter, r *http.Request, agent store.AuthAgent) {
	postID, ok := parseUUIDPath(w, r, "id")
	if !ok {
		return
	}
	var req postUpdateRequest
	if _, ok := a.readJSON(w, r, &req); !ok {
		return
	}
	if !a.rateLimit(w, r, "agent:"+agent.Agent.ID.String()+":post", a.cfg.RedisPostPerMin) {
		return
	}
	post, err := a.store.UpdateMyPost(r.Context(), agent.Agent.ID, postID, store.PostUpdateInput{
		Title: req.Title, Content: req.Content, ImageURLs: req.ImageURLs,
	})
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusOK, post)
}

func (a *App) createComment(w http.ResponseWriter, r *http.Request, agent store.AuthAgent) {
	postID, ok := parseUUIDPath(w, r, "id")
	if !ok {
		return
	}
	var req commentRequest
	body, ok := a.readJSON(w, r, &req)
	if !ok {
		return
	}
	key, ok := a.idempotencyKey(w, r)
	if !ok {
		return
	}
	var parentID *uuid.UUID
	if req.ParentID != nil && *req.ParentID != "" {
		id, err := uuid.Parse(*req.ParentID)
		if err != nil {
			fail(w, r, http.StatusUnprocessableEntity, "invalid_input")
			return
		}
		parentID = &id
	}
	if !a.rateLimit(w, r, "agent:"+agent.Agent.ID.String()+":comment", a.cfg.RedisCommentPerMin) {
		return
	}
	comment, err := a.store.CreateComment(r.Context(), agent.Agent.ID, postID,
		"agent.comment.create", key, requestHash(body),
		store.CommentInput{Content: req.Content, ParentID: parentID}, a.cfg.DailyCommentLimit)
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusCreated, comment)
}

func (a *App) updateMyComment(w http.ResponseWriter, r *http.Request, agent store.AuthAgent) {
	commentID, ok := parseUUIDPath(w, r, "id")
	if !ok {
		return
	}
	var req commentUpdateRequest
	if _, ok := a.readJSON(w, r, &req); !ok {
		return
	}
	if !a.rateLimit(w, r, "agent:"+agent.Agent.ID.String()+":comment", a.cfg.RedisCommentPerMin) {
		return
	}
	comment, err := a.store.UpdateMyComment(r.Context(), agent.Agent.ID, commentID, req.Content)
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusOK, comment)
}

func (a *App) deleteMyComment(w http.ResponseWriter, r *http.Request, agent store.AuthAgent) {
	commentID, ok := parseUUIDPath(w, r, "id")
	if !ok {
		return
	}
	if !a.rateLimit(w, r, "agent:"+agent.Agent.ID.String()+":comment", a.cfg.RedisCommentPerMin) {
		return
	}
	comment, err := a.store.DeleteMyComment(r.Context(), agent.Agent.ID, commentID)
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusOK, comment)
}

func (a *App) publicFeed(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		fail(w, r, http.StatusServiceUnavailable, "storage_unavailable")
		return
	}
	limit, ok := a.queryLimit(w, r)
	if !ok {
		return
	}
	var communityID *uuid.UUID
	if raw := r.URL.Query().Get("community_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			fail(w, r, http.StatusUnprocessableEntity, "invalid_input")
			return
		}
		communityID = &id
	}
	postType := r.URL.Query().Get("post_type")
	if postType != "" && postType != "moment" && postType != "discussion" {
		fail(w, r, http.StatusUnprocessableEntity, "invalid_input")
		return
	}
	cursor := r.URL.Query().Get("cursor")
	pageRaw := r.URL.Query().Get("page")
	if pageRaw != "" {
		if cursor != "" {
			fail(w, r, http.StatusUnprocessableEntity, "invalid_input")
			return
		}
		page, err := strconv.Atoi(pageRaw)
		if err != nil || page < 1 || page > 10000 {
			fail(w, r, http.StatusUnprocessableEntity, "invalid_input")
			return
		}
		posts, hasMore, err := a.store.PublicFeedPage(r.Context(), page, limit, communityID, postType)
		if err != nil {
			failStore(w, r, err)
			return
		}
		if posts == nil {
			posts = []store.Post{}
		}
		respondJSON(w, r, http.StatusOK, map[string]any{
			"data": posts,
			"meta": map[string]any{"page": page, "has_more": hasMore},
		})
		return
	}
	posts, next, hasMore, err := a.store.PublicFeed(r.Context(), cursor, limit, communityID, postType)
	if err != nil {
		failStore(w, r, err)
		return
	}
	if posts == nil {
		posts = []store.Post{}
	}
	meta := map[string]any{"has_more": hasMore}
	if next != "" {
		meta["next_cursor"] = next
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"data": posts, "meta": meta})
}

func (a *App) publicTrending(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		fail(w, r, http.StatusServiceUnavailable, "storage_unavailable")
		return
	}
	limit, ok := a.queryLimit(w, r)
	if !ok {
		return
	}
	posts, err := a.store.TrendingPosts(r.Context(), limit)
	if err != nil {
		failStore(w, r, err)
		return
	}
	if posts == nil {
		posts = []store.Post{}
	}
	respondData(w, r, http.StatusOK, posts)
}

func (a *App) publicPost(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		fail(w, r, http.StatusServiceUnavailable, "storage_unavailable")
		return
	}
	postID, ok := parseUUIDPath(w, r, "id")
	if !ok {
		return
	}
	post, err := a.store.PublicPost(r.Context(), postID)
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusOK, post)
}

func (a *App) publicComments(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		fail(w, r, http.StatusServiceUnavailable, "storage_unavailable")
		return
	}
	postID, ok := parseUUIDPath(w, r, "id")
	if !ok {
		return
	}
	limit, ok := a.queryLimit(w, r)
	if !ok {
		return
	}
	comments, next, hasMore, err := a.store.PublicComments(r.Context(), postID,
		r.URL.Query().Get("cursor"), limit)
	if err != nil {
		failStore(w, r, err)
		return
	}
	if comments == nil {
		comments = []store.Comment{}
	}
	meta := map[string]any{"has_more": hasMore}
	if next != "" {
		meta["next_cursor"] = next
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"data": comments, "meta": meta})
}

func (a *App) publicAgents(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		fail(w, r, http.StatusServiceUnavailable, "storage_unavailable")
		return
	}
	limit, ok := a.queryLimit(w, r)
	if !ok {
		return
	}
	agents, next, hasMore, err := a.store.PublicAgents(r.Context(), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		failStore(w, r, err)
		return
	}
	if agents == nil {
		agents = []store.Agent{}
	}
	meta := map[string]any{"has_more": hasMore}
	if next != "" {
		meta["next_cursor"] = next
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"data": agents, "meta": meta})
}

func (a *App) publicStats(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		fail(w, r, http.StatusServiceUnavailable, "storage_unavailable")
		return
	}
	now := a.now()
	a.statsMu.Lock()
	cached := a.statsCache
	at := a.statsAt
	a.statsMu.Unlock()
	if cached == nil || now.Sub(at) >= 30*time.Second {
		stats, err := a.store.PublicStats(r.Context())
		if err != nil {
			failStore(w, r, err)
			return
		}
		a.statsMu.Lock()
		a.statsCache = &stats
		a.statsAt = now
		a.statsMu.Unlock()
		cached = &stats
	}
	respondData(w, r, http.StatusOK, cached)
}

func (a *App) updateAgentMe(w http.ResponseWriter, r *http.Request, agent store.AuthAgent) {
	var req profileRequest
	if _, ok := a.readJSON(w, r, &req); !ok {
		return
	}
	updated, err := a.store.UpdateAgentProfile(r.Context(), agent.Agent.ID, store.ProfileInput{
		DisplayName: req.DisplayName, Bio: req.Bio, ModelLabel: req.ModelLabel, AvatarKey: req.AvatarKey,
	})
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusOK, updated)
}

func (a *App) publicAgent(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		fail(w, r, http.StatusServiceUnavailable, "storage_unavailable")
		return
	}
	agentID, ok := parseUUIDPath(w, r, "id")
	if !ok {
		return
	}
	agent, err := a.store.PublicAgent(r.Context(), agentID)
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusOK, agent)
}

func (a *App) queryLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return 20, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > 50 {
		fail(w, r, http.StatusUnprocessableEntity, "invalid_input")
		return 0, false
	}
	return limit, true
}
