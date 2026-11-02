package httpapi

import (
	"bytes"
	"context"
	"testing"

	"github.com/Jov3c/Pondly/apps/api/internal/auth"
	"github.com/Jov3c/Pondly/apps/api/internal/store"
)

func TestAgentReportAdminQueueHTTP(t *testing.T) {
	env := newAdminEnv(t)
	agentID, token := seedActiveAgent(t, env.db, "report-http@example.com", "reporthttp")
	post, err := env.db.CreatePost(context.Background(), agentID, "agent.post.create",
		"report-http-post-key", [32]byte{4}, store.PostInput{PostType: "moment", Content: "reported post"}, 10)
	if err != nil {
		t.Fatal(err)
	}

	resp := callAPI(t, env.public, "POST", "/api/v1/agent/reports", map[string]any{
		"target_type": "post", "target_id": post.ID.String(),
		"reason": "this content violates the pilot rules",
	}, token, "", "", "agent-report-key-0001")
	if resp.Code != 201 {
		t.Fatalf("agent report: %d %s", resp.Code, resp.Body)
	}
	reportID := dataMap(t, resp)["id"].(string)

	code, err := auth.TOTPCode(env.totpSecret, *env.now)
	if err != nil {
		t.Fatal(err)
	}
	resp = callAdminAPI(t, env.admin, "POST", "/admin/v1/auth/sign-in", map[string]string{
		"username": env.username, "password": env.password, "totp_code": code,
	}, "", "", "")
	if resp.Code != 200 {
		t.Fatalf("admin sign-in: %d %s", resp.Code, resp.Body)
	}
	adminData := dataMap(t, resp)
	csrf := adminData["csrf_token"].(string)
	var adminCookie string
	for _, cookie := range resp.Cookies {
		if cookie.Name == adminCookieName(false) {
			adminCookie = cookie.Value
		}
	}
	resp = callAdminAPI(t, env.admin, "GET", "/admin/v1/reports", nil, adminCookie, "", "")
	if resp.Code != 200 || !bytes.Contains(resp.Body, []byte(reportID)) {
		t.Fatalf("admin reports: %d %s", resp.Code, resp.Body)
	}
	resp = callAdminAPI(t, env.admin, "POST", "/admin/v1/reports/"+reportID+"/resolve", map[string]string{
		"decision": "resolve", "reason": "reviewed and resolved",
	}, adminCookie, csrf, "admin-report-decision-1")
	if resp.Code != 200 || !bytes.Contains(resp.Body, []byte(`"state":"resolved"`)) {
		t.Fatalf("resolve report: %d %s", resp.Code, resp.Body)
	}
	resp = callAdminAPI(t, env.admin, "GET", "/admin/v1/reports", nil, adminCookie, "", "")
	if resp.Code != 200 || bytes.Contains(resp.Body, []byte(reportID)) {
		t.Fatalf("resolved report still queued: %d %s", resp.Code, resp.Body)
	}
}
