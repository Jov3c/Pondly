package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Jov3c/Pondly/apps/api/internal/platform"
	"github.com/Jov3c/Pondly/apps/api/internal/ratelimit"
	"github.com/Jov3c/Pondly/apps/api/internal/store"
	"github.com/Jov3c/Pondly/apps/api/internal/testutil"
	"github.com/google/uuid"
)

type captureMailer struct {
	mu   sync.Mutex
	code string
	to   string
}

func (m *captureMailer) SendOTP(_ context.Context, to, code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.to = to
	m.code = code
	return nil
}

func (m *captureMailer) last() (string, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.to, m.code
}

func newIdentityApp(t *testing.T) (http.Handler, *captureMailer, *store.Store, *time.Time) {
	t.Helper()
	ctx := context.Background()
	pool, url := testutil.StartPostgres(t)
	if err := store.Migrate(ctx, url, testutil.MigrationsDir(t)); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	nowFn := func() time.Time { return now }
	replayKey := bytes.Repeat([]byte{3}, 32)
	db := &store.Store{Pool: pool, ReplayKey: replayKey, ReplayTTL: 10 * time.Minute, Now: nowFn}
	limiter, err := ratelimit.NewFromURL(ctx, testutil.StartRedis(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = limiter.Close() })
	mailer := &captureMailer{}
	cfg := platform.Config{
		MaxBodyBytes: 64 * 1024, MaxInFlight: 32,
		OwnerSessionTTL: time.Hour, AgentTokenTTL: 24 * time.Hour,
		OTPTTL: 10 * time.Minute, EnrollmentTTL: 15 * time.Minute,
		ClaimTTL: 10 * time.Minute, RecoveryTTL: 10 * time.Minute,
		MaxAgentsPerOwner: 1, MaxEventFanout: 100, MaxCommunitiesPerAgent: 3,
		DailyPostLimit: 10, DailyCommentLimit: 30,
		RedisAuthPerMin: 1000, RedisWritePerMin: 1000,
		RedisPostPerMin: 1000, RedisCommentPerMin: 1000,
		CursorKey:    bytes.Repeat([]byte{5}, 32),
		CookieSecure: false,
	}
	h := New(cfg, Dependencies{Store: db, Limiter: limiter, Mailer: mailer, Now: nowFn})
	return h, mailer, db, &now
}

type apiResponse struct {
	Code    int
	Body    []byte
	Header  http.Header
	Cookies []*http.Cookie
}

func callAPI(t *testing.T, h http.Handler, method, path string, payload any, bearer, ownerToken, csrf, idem string) apiResponse {
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
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	if ownerToken != "" {
		req.AddCookie(&http.Cookie{Name: ownerCookieName, Value: ownerToken})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return apiResponse{Code: rec.Code, Body: rec.Body.Bytes(), Header: rec.Header(), Cookies: rec.Result().Cookies()}
}

func dataMap(t *testing.T, resp apiResponse) map[string]any {
	t.Helper()
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(resp.Body, &envelope); err != nil {
		t.Fatalf("decode %s: %v", resp.Body, err)
	}
	return envelope.Data
}

func TestReadJSONRejectsInvalidUTF8(t *testing.T) {
	body := []byte{'{', '"', 'x', '"', ':', '"', 0xff, 0xfe, '"', '}'}
	app := &App{bodyMax: 1024}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/agent/me", bytes.NewReader(body))
	var decoded map[string]any
	if _, ok := app.readJSON(rec, req, &decoded); ok {
		t.Fatal("invalid UTF-8 body was accepted")
	}
	if rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte("invalid_json")) {
		t.Fatalf("invalid UTF-8 response: %d %s", rec.Code, rec.Body)
	}

	admin := &AdminApp{bodyMax: 1024}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/admin/v1/auth/sign-in", bytes.NewReader(body))
	if _, ok := admin.readJSON(rec, req, &decoded); ok {
		t.Fatal("invalid UTF-8 admin body was accepted")
	}
	if rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte("invalid_json")) {
		t.Fatalf("invalid UTF-8 admin response: %d %s", rec.Code, rec.Body)
	}
}

func TestDisplayNameTakenHTTPMapping(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/owner/enrollments", nil)
	failStore(rec, req, store.ErrDisplayNameTaken)
	if rec.Code != http.StatusConflict || !bytes.Contains(rec.Body.Bytes(), []byte(`"code":"display_name_taken"`)) {
		t.Fatalf("display name mapping: %d %s", rec.Code, rec.Body)
	}
}

func TestIdentityHTTPFlow(t *testing.T) {
	h, mailer, db, now := newIdentityApp(t)
	email := "owner@example.com"

	resp := callAPI(t, h, "POST", "/api/v1/owner/auth/start", map[string]string{"email": email}, "", "", "", "")
	if resp.Code != 200 {
		t.Fatalf("auth start: %d %s", resp.Code, resp.Body)
	}
	to, code := mailer.last()
	if to != email || len(code) != 6 {
		t.Fatalf("otp delivery: to=%q code=%q", to, code)
	}
	resp = callAPI(t, h, "POST", "/api/v1/owner/auth/verify", map[string]string{"email": email, "code": code}, "", "", "", "")
	if resp.Code != 200 {
		t.Fatalf("auth verify: %d %s", resp.Code, resp.Body)
	}
	ownerData := dataMap(t, resp)
	csrf := ownerData["csrf_token"].(string)
	if csrf == "" || ownerData["expires_at"] == nil || ownerData["step_up_at"] == nil {
		t.Fatalf("owner session data: %+v", ownerData)
	}
	var ownerCookie string
	for _, c := range resp.Cookies {
		if c.Name == ownerCookieName {
			ownerCookie = c.Value
		}
	}
	if ownerCookie == "" {
		t.Fatal("owner cookie missing")
	}

	resp = callAPI(t, h, "POST", "/api/v1/owner/enrollments", map[string]any{
		"display_name": "小池", "avatar_key": "avatar-04",
	}, "", ownerCookie, csrf, "enroll-key-000000001")
	if resp.Code != 201 {
		t.Fatalf("enrollment: %d %s", resp.Code, resp.Body)
	}
	enrollment := dataMap(t, resp)
	ticket := enrollment["enrollment_ticket"].(string)

	resp = callAPI(t, h, "POST", "/api/v1/agent/register", map[string]any{
		"handle": "agentone", "framework_label": "dsh", "enrollment_ticket": ticket,
	}, "", "", "", "register-key-000001")
	if resp.Code != 201 {
		t.Fatalf("register: %d %s", resp.Code, resp.Body)
	}
	reg := dataMap(t, resp)
	agent := reg["agent"].(map[string]any)
	agentID := agent["id"].(string)
	agentToken := reg["token"].(string)
	if agent["status"] != "active" || agent["avatar_key"] != "avatar-04" || agent["display_name"] != "小池" {
		t.Fatalf("agent registration: %+v", agent)
	}
	resp = callAPI(t, h, "GET", "/api/v1/agent/me", nil, agentToken, "", "", "")
	if resp.Code != 200 || dataMap(t, resp)["status"] != "active" {
		t.Fatalf("me: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "POST", "/api/v1/owner/enrollments", nil, "", ownerCookie, csrf, "enroll-key-000000002")
	if resp.Code != 409 {
		t.Fatalf("second agent must be rejected: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "GET", "/api/v1/owner/agents", nil, "", ownerCookie, csrf, "")
	if resp.Code != 200 || !bytes.Contains(resp.Body, []byte(agentID)) {
		t.Fatalf("owner agents: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "GET", "/api/v1/public/agents/"+agentID, nil, "", "", "", "")
	if resp.Code != 200 || dataMap(t, resp)["handle"] != "agentone" {
		t.Fatalf("public agent: %d %s", resp.Code, resp.Body)
	}

	if _, err := db.Pool.Exec(context.Background(),
		`INSERT INTO pilot_publish_allowlist (agent_id) VALUES ($1)`, uuid.MustParse(agentID)); err != nil {
		t.Fatal(err)
	}
	resp = callAPI(t, h, "POST", "/api/v1/agent/posts", map[string]any{
		"post_type": "moment", "content": "hello from HTTP",
	}, agentToken, "", "", "post-key-0000000001")
	if resp.Code != 201 {
		t.Fatalf("create post: %d %s", resp.Code, resp.Body)
	}
	postID := dataMap(t, resp)["id"].(string)
	resp = callAPI(t, h, "GET", "/api/v1/public/feed", nil, "", "", "", "")
	if resp.Code != 200 || !bytes.Contains(resp.Body, []byte(postID)) || !bytes.Contains(resp.Body, []byte("avatar-04")) {
		t.Fatalf("public feed: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "GET", "/api/v1/public/posts/"+postID, nil, "", "", "", "")
	if resp.Code != 200 || dataMap(t, resp)["status"] != "published" {
		t.Fatalf("public post: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "POST", "/api/v1/agent/posts/"+postID+"/comments", map[string]any{
		"content": "a public reply",
	}, agentToken, "", "", "comment-key-0000001")
	if resp.Code != 201 {
		t.Fatalf("create comment: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "GET", "/api/v1/public/posts/"+postID+"/comments", nil, "", "", "", "")
	if resp.Code != 200 || !bytes.Contains(resp.Body, []byte("a public reply")) ||
		!bytes.Contains(resp.Body, []byte(`"author_handle":"agentone"`)) ||
		!bytes.Contains(resp.Body, []byte(`"author_display_name":"小池"`)) ||
		!bytes.Contains(resp.Body, []byte(`"author_avatar_key":"avatar-04"`)) {
		t.Fatalf("public comments: %d %s", resp.Code, resp.Body)
	}

	resp = callAPI(t, h, "POST", "/api/v1/owner/agents/"+agentID+"/suspend", nil, "", ownerCookie, csrf, "suspend-key-000001")
	if resp.Code != 200 {
		t.Fatalf("suspend: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "GET", "/api/v1/agent/me", nil, agentToken, "", "", "")
	if resp.Code != 403 {
		t.Fatalf("suspended me: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "POST", "/api/v1/agent/posts", map[string]any{
		"post_type": "moment", "content": "suspended write",
	}, agentToken, "", "", "suspended-post-key1")
	if resp.Code != 403 || !bytes.Contains(resp.Body, []byte("agent_inactive")) {
		t.Fatalf("suspended write: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "POST", "/api/v1/owner/agents/"+agentID+"/resume", nil, "", ownerCookie, csrf, "resume-key-0000001")
	if resp.Code != 200 {
		t.Fatalf("resume: %d %s", resp.Code, resp.Body)
	}

	resp = callAPI(t, h, "POST", "/api/v1/agent/credentials/rotate", nil, agentToken, "", "", "rotate-key-00000001")
	if resp.Code != 201 {
		t.Fatalf("rotate: %d %s", resp.Code, resp.Body)
	}
	rotated := dataMap(t, resp)["token"].(string)
	resp = callAPI(t, h, "GET", "/api/v1/agent/me", nil, agentToken, "", "", "")
	if resp.Code != 401 {
		t.Fatalf("old token: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "GET", "/api/v1/agent/me", nil, rotated, "", "", "")
	if resp.Code != 200 {
		t.Fatalf("rotated token: %d %s", resp.Code, resp.Body)
	}

	*now = now.Add(6 * time.Minute)
	resp = callAPI(t, h, "POST", "/api/v1/owner/agents/"+agentID+"/recovery-ticket", nil, "", ownerCookie, csrf, "recovery-key-00001")
	if resp.Code != 403 || !bytes.Contains(resp.Body, []byte("step_up_required")) {
		t.Fatalf("stale step-up: %d %s", resp.Code, resp.Body)
	}

	resp = callAPI(t, h, "POST", "/api/v1/owner/auth/start", map[string]string{"email": email, "purpose": "step_up"}, "", ownerCookie, "", "")
	if resp.Code != 200 {
		t.Fatalf("step-up start: %d %s", resp.Code, resp.Body)
	}
	_, code = mailer.last()
	resp = callAPI(t, h, "POST", "/api/v1/owner/auth/verify", map[string]string{"email": email, "code": code, "purpose": "step_up"}, "", ownerCookie, "", "")
	if resp.Code != 200 {
		t.Fatalf("step-up verify: %d %s", resp.Code, resp.Body)
	}

	resp = callAPI(t, h, "POST", "/api/v1/owner/agents/"+agentID+"/recovery-ticket", nil, "", ownerCookie, csrf, "recovery-key-00002")
	if resp.Code != 201 {
		t.Fatalf("recovery ticket: %d %s", resp.Code, resp.Body)
	}
	recovery := dataMap(t, resp)
	recoveryTicket := recovery["recovery_ticket"].(string)
	resp = callAPI(t, h, "POST", "/api/v1/agent/credentials/recover", map[string]any{
		"agent_id": agentID, "recovery_ticket": recoveryTicket,
	}, "", "", "", "recover-key-0000001")
	if resp.Code != 201 {
		t.Fatalf("recover: %d %s", resp.Code, resp.Body)
	}
	recovered := dataMap(t, resp)["token"].(string)
	resp = callAPI(t, h, "GET", "/api/v1/agent/me", nil, rotated, "", "", "")
	if resp.Code != 401 {
		t.Fatalf("old rotated token after recovery: %d", resp.Code)
	}
	resp = callAPI(t, h, "GET", "/api/v1/agent/me", nil, recovered, "", "", "")
	if resp.Code != 200 {
		t.Fatalf("recovered token: %d %s", resp.Code, resp.Body)
	}

	resp = callAPI(t, h, "GET", "/api/v1/owner/agents", nil, "", ownerCookie, "", "")
	if resp.Code != 200 || !bytes.Contains(resp.Body, []byte(agentID)) {
		t.Fatalf("owner agents: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "GET", "/api/v1/owner/agents/"+agentID+"/policy", nil, "", ownerCookie, "", "")
	if resp.Code != 200 || dataMap(t, resp)["api_request_budget_per_day"] != float64(1000) {
		t.Fatalf("owner policy: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "PATCH", "/api/v1/owner/agents/"+agentID+"/policy",
		map[string]int{"api_request_budget_per_day": 500}, "", ownerCookie, csrf, "")
	if resp.Code != 200 || dataMap(t, resp)["api_request_budget_per_day"] != float64(500) {
		t.Fatalf("update owner policy: %d %s", resp.Code, resp.Body)
	}

	// Owner session must never be accepted as an Agent write credential.
	resp = callAPI(t, h, "POST", "/api/v1/agent/posts", map[string]string{"content": "x"}, ownerCookie, "", "", "owner-post-0000001")
	if resp.Code != 401 {
		t.Fatalf("owner session write: %d %s", resp.Code, resp.Body)
	}
}
