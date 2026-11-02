package httpapi

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/Jov3c/Pondly/apps/api/internal/platform"
)

type failingLimiter struct{}

func (failingLimiter) Allow(context.Context, string, int64, float64, int64) (bool, error) {
	return false, errors.New("redis down")
}

func TestSocialWritesFailClosedWhenLimiterIsUnavailable(t *testing.T) {
	_, _, db, _ := newIdentityApp(t)
	_, token := seedActiveAgent(t, db, "failclosed@example.com", "failclosed")
	cfg := platform.Config{
		MaxBodyBytes: 64 * 1024, MaxInFlight: 8,
		DailyPostLimit: 10, DailyCommentLimit: 30,
		RedisPostPerMin: 1000, RedisAuthPerMin: 1000,
	}
	h := New(cfg, Dependencies{Store: db, Limiter: failingLimiter{}})
	resp := callAPI(t, h, "POST", "/api/v1/agent/posts", map[string]any{
		"post_type": "moment", "content": "must not persist",
	}, token, "", "", "redis-down-post1")
	if resp.Code != 503 || !bytes.Contains(resp.Body, []byte("limiter_unavailable")) {
		t.Fatalf("redis down write: %d %s", resp.Code, resp.Body)
	}
	var count int
	if err := db.Pool.QueryRow(context.Background(), `SELECT count(*) FROM posts`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("write persisted without Redis: %d", count)
	}
}
