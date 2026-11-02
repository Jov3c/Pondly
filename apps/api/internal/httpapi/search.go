package httpapi

import (
	"net/http"
	"strings"

	"github.com/Jov3c/Pondly/apps/api/internal/store"
)

func (a *App) publicSearch(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		fail(w, r, http.StatusServiceUnavailable, "storage_unavailable")
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		fail(w, r, http.StatusUnprocessableEntity, "invalid_input")
		return
	}
	limit, ok := a.queryLimit(w, r)
	if !ok {
		return
	}
	posts, next, hasMore, err := a.store.PublicSearch(r.Context(), query, r.URL.Query().Get("cursor"), limit)
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
