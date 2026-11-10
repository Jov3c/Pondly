package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Jov3c/Pondly/apps/api/internal/auth"
	"github.com/Jov3c/Pondly/apps/api/internal/platform"
	"github.com/Jov3c/Pondly/apps/api/internal/ratelimit"
	"github.com/Jov3c/Pondly/apps/api/internal/store"
	"github.com/Jov3c/Pondly/apps/api/internal/testutil"
	"github.com/google/uuid"
)

type adminEnv struct {
	admin      http.Handler
	public     http.Handler
	db         *store.Store
	now        *time.Time
	username   string
	password   string
	totpSecret string
	adminID    uuid.UUID
}

func newAdminEnv(t *testing.T) adminEnv {
	t.Helper()
	ctx := context.Background()
	pool, url := testutil.StartPostgres(t)
	if err := store.Migrate(ctx, url, testutil.MigrationsDir(t)); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	nowFn := func() time.Time { return now }
	db := &store.Store{Pool: pool, ReplayKey: bytes.Repeat([]byte{8}, 32), ReplayTTL: 10 * time.Minute, Now: nowFn}
	limiter, err := ratelimit.NewFromURL(ctx, testutil.StartRedis(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = limiter.Close() })
	cfg := platform.Config{
		MaxBodyBytes: 64 * 1024, MaxInFlight: 32, AdminSessionTTL: 8 * time.Hour,
		DailyPostLimit: 10, DailyCommentLimit: 30,
		RedisAuthPerMin: 1000, RedisWritePerMin: 1000,
		RedisPostPerMin: 1000, RedisCommentPerMin: 1000,
		CursorKey: bytes.Repeat([]byte{6}, 32), CookieSecure: false,
	}
	deps := Dependencies{Store: db, Limiter: limiter, Now: nowFn}
	username := "admin-" + uuid.NewString()[:8]
	password := "correct horse battery staple"
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := auth.NewTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	adminID, err := db.CreateAdminAccount(ctx, username, hash, secret)
	if err != nil {
		t.Fatal(err)
	}
	return adminEnv{
		admin: NewAdmin(cfg, deps), public: New(cfg, deps), db: db, now: &now,
		username: username, password: password, totpSecret: secret, adminID: adminID,
	}
}

func callAdminAPI(t *testing.T, h http.Handler, method, path string, payload any, cookie, csrf, idem string) apiResponse {
	t.Helper()
	var body []byte
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: adminCookieName(false), Value: cookie})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return apiResponse{Code: rec.Code, Body: rec.Body.Bytes(), Header: rec.Header(), Cookies: rec.Result().Cookies()}
}

func adminSession(t *testing.T, env adminEnv) (string, string) {
	t.Helper()
	code, err := auth.TOTPCode(env.totpSecret, *env.now)
	if err != nil {
		t.Fatal(err)
	}
	resp := callAdminAPI(t, env.admin, "POST", "/admin/v1/auth/sign-in", map[string]string{
		"username": env.username, "password": env.password, "totp_code": code,
	}, "", "", "")
	if resp.Code != 200 {
		t.Fatalf("admin sign-in: %d %s", resp.Code, resp.Body)
	}
	var cookie string
	for _, c := range resp.Cookies {
		if c.Name == adminCookieName(false) {
			cookie = c.Value
		}
	}
	if cookie == "" {
		t.Fatal("admin cookie missing")
	}
	return cookie, dataMap(t, resp)["csrf_token"].(string)
}

func TestAdminConsoleShowsPendingQueue(t *testing.T) {
	env := newAdminEnv(t)
	agentID, _ := seedActiveAgent(t, env.db, "console@example.com", "consoleadmin")
	pendingPost, err := env.db.CreatePost(context.Background(), agentID, "agent.post.create",
		"console-post-key", [32]byte{1},
		store.PostInput{PostType: "moment", Content: "<script>alert(1)</script> pending console"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	basePost, err := env.db.CreatePost(context.Background(), agentID, "agent.post.create",
		"console-base-key", [32]byte{2}, store.PostInput{PostType: "moment", Content: "base post"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.AdminDecidePost(context.Background(), env.adminID, basePost.ID,
		"approve", "approve base post", [32]byte{3}, "admin.post.decision", "console-base-approve-1"); err != nil {
		t.Fatal(err)
	}
	pendingComment, err := env.db.CreateComment(context.Background(), agentID, basePost.ID,
		"agent.comment.create", "console-comment-key", [32]byte{4},
		store.CommentInput{Content: "pending console comment"}, 10)
	if err != nil {
		t.Fatal(err)
	}

	resp := callAdminAPI(t, env.admin, "GET", "/admin/", nil, "", "", "")
	if resp.Code != 200 || !bytes.Contains(resp.Body, []byte("管理登录")) {
		t.Fatalf("admin login page: %d %s", resp.Code, resp.Body)
	}
	if bytes.Contains(resp.Body, []byte("pending console comment")) {
		t.Fatal("login page leaked pending content")
	}

	adminCookie, _ := adminSession(t, env)

	resp = callAdminAPI(t, env.admin, "GET", "/admin/", nil, adminCookie, "", "")
	body := string(resp.Body)
	if resp.Code != 200 {
		t.Fatalf("admin console: %d %s", resp.Code, body)
	}
	for _, want := range []string{
		"待审核", `data-kind="posts"`, `data-kind="comments"`,
		`<meta name="csrf-token"`, pendingPost.ID.String(), pendingComment.ID.String(),
		"pending console comment", "&lt;script&gt;alert(1)&lt;/script&gt;",
	} {
		if !bytes.Contains(resp.Body, []byte(want)) {
			t.Fatalf("admin console missing %q", want)
		}
	}
	if bytes.Contains(resp.Body, []byte("<script>alert(1)</script>")) {
		t.Fatal("pending content was not escaped")
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !bytes.Contains([]byte(csp), []byte("script-src 'self'")) {
		t.Fatalf("missing CSP: %q", csp)
	}

	asset := callAdminAPI(t, env.admin, "GET", "/admin/admin.css", nil, "", "", "")
	if asset.Code != 200 || !bytes.Contains(asset.Body, []byte("--paper")) {
		t.Fatalf("admin css: %d %s", asset.Code, asset.Body)
	}
}

func TestAdminHTTPPrivateModeration(t *testing.T) {
	env := newAdminEnv(t)
	agentID, _ := seedActiveAgent(t, env.db, "moderated-http@example.com", "moderatedhttp")
	post, err := env.db.CreatePost(context.Background(), agentID, "agent.post.create",
		"admin-http-post-key", [32]byte{1}, store.PostInput{PostType: "moment", Content: "pending http"}, 10)
	if err != nil {
		t.Fatal(err)
	}

	// Admin routes must not exist on the public gateway.
	resp := callAPI(t, env.public, "POST", "/admin/v1/auth/sign-in", map[string]string{
		"username": env.username, "password": env.password, "totp_code": "000000",
	}, "", "", "", "")
	if resp.Code != 404 {
		t.Fatalf("public admin route: %d %s", resp.Code, resp.Body)
	}

	code, err := auth.TOTPCode(env.totpSecret, *env.now)
	if err != nil {
		t.Fatal(err)
	}
	resp = callAdminAPI(t, env.admin, "POST", "/admin/v1/auth/sign-in", map[string]string{
		"username": env.username, "password": "wrong password value", "totp_code": code,
	}, "", "", "")
	if resp.Code != 401 {
		t.Fatalf("wrong password: %d %s", resp.Code, resp.Body)
	}
	adminCookie, csrf := adminSession(t, env)

	resp = callAdminAPI(t, env.admin, "POST", "/admin/v1/posts/"+post.ID.String()+"/decision", map[string]string{
		"decision": "approve", "reason": "pilot approval",
	}, adminCookie, "", "admin-decision-key-1")
	if resp.Code != 403 {
		t.Fatalf("missing csrf: %d %s", resp.Code, resp.Body)
	}
	resp = callAdminAPI(t, env.admin, "POST", "/admin/v1/posts/"+post.ID.String()+"/decision", map[string]string{
		"decision": "approve", "reason": "pilot approval",
	}, adminCookie, csrf, "admin-decision-key-1")
	if resp.Code != 200 {
		t.Fatalf("admin decision: %d %s", resp.Code, resp.Body)
	}
	if dataMap(t, resp)["status"] != "published" {
		t.Fatalf("decision response: %s", resp.Body)
	}

	resp = callAPI(t, env.public, "GET", "/api/v1/public/posts/"+post.ID.String(), nil, "", "", "", "")
	if resp.Code != 200 || dataMap(t, resp)["status"] != "published" {
		t.Fatalf("public post after approval: %d %s", resp.Code, resp.Body)
	}

	comment, err := env.db.CreateComment(context.Background(), agentID, post.ID, "agent.comment.create",
		"admin-http-comment-key", [32]byte{2}, store.CommentInput{Content: "pending comment"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	resp = callAdminAPI(t, env.admin, "POST", "/admin/v1/comments/"+comment.ID.String()+"/decision", map[string]string{
		"decision": "approve", "reason": "pilot comment approval",
	}, adminCookie, csrf, "admin-comment-decision-1")
	if resp.Code != 200 || dataMap(t, resp)["status"] != "published" {
		t.Fatalf("admin comment decision: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, env.public, "GET", "/api/v1/public/posts/"+post.ID.String()+"/comments", nil, "", "", "", "")
	if resp.Code != 200 || !bytes.Contains(resp.Body, []byte("pending comment")) {
		t.Fatalf("public comment after approval: %d %s", resp.Code, resp.Body)
	}

	resp = callAdminAPI(t, env.admin, "POST", "/admin/v1/agents/"+agentID.String()+"/suspend", map[string]string{
		"decision": "suspend", "reason": "risk control suspension",
	}, adminCookie, csrf, "admin-suspend-key-1")
	if resp.Code != 200 || dataMap(t, resp)["status"] != "disabled" {
		t.Fatalf("admin suspend: %d %s", resp.Code, resp.Body)
	}

	resp = callAdminAPI(t, env.admin, "POST", "/admin/v1/auth/sign-out", nil, adminCookie, csrf, "")
	if resp.Code != 200 {
		t.Fatalf("sign-out: %d %s", resp.Code, resp.Body)
	}
	resp = callAdminAPI(t, env.admin, "POST", "/admin/v1/posts/"+post.ID.String()+"/decision", map[string]string{
		"decision": "approve", "reason": "pilot approval",
	}, adminCookie, csrf, "admin-decision-key-2")
	if resp.Code != 401 {
		t.Fatalf("revoked admin session: %d %s", resp.Code, resp.Body)
	}
}

func TestAdminDecisionEnforcesReasonBounds(t *testing.T) {
	env := newAdminEnv(t)
	cookie, csrf := adminSession(t, env)
	agentID, _ := seedActiveAgent(t, env.db, "reason-bounds@example.com", "reasonbounds")
	post, err := env.db.CreatePost(context.Background(), agentID, "agent.post.create",
		"reason-bounds-post", [32]byte{1}, store.PostInput{PostType: "moment", Content: "pending reason bounds"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	basePost, err := env.db.CreatePost(context.Background(), agentID, "agent.post.create",
		"reason-bounds-base", [32]byte{2}, store.PostInput{PostType: "moment", Content: "base post"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.AdminDecidePost(context.Background(), env.adminID, basePost.ID,
		"approve", "approve base post", [32]byte{3}, "admin.post.decision", "reason-bounds-base-approve"); err != nil {
		t.Fatal(err)
	}
	comment, err := env.db.CreateComment(context.Background(), agentID, basePost.ID,
		"agent.comment.create", "reason-bounds-comment", [32]byte{4}, store.CommentInput{Content: "pending reason bounds"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		runes int
		want  int
	}{
		{"short", 9, http.StatusUnprocessableEntity},
		{"over", 1001, http.StatusUnprocessableEntity},
		{"cap", 1000, http.StatusOK},
	}
	for _, target := range []struct {
		path string
		idem string
	}{
		{"/admin/v1/posts/" + post.ID.String() + "/decision", "reason-bound-post-"},
		{"/admin/v1/comments/" + comment.ID.String() + "/decision", "reason-bound-comment-"},
	} {
		for _, tc := range cases {
			resp := callAdminAPI(t, env.admin, "POST", target.path, map[string]string{
				"decision": "approve", "reason": strings.Repeat("理", tc.runes),
			}, cookie, csrf, target.idem+tc.name)
			if resp.Code != tc.want {
				t.Fatalf("%s reason %d runes: %d %s", target.path, tc.runes, resp.Code, resp.Body)
			}
		}
	}
}
