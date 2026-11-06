package httpapi

import (
	"net/http"
	"testing"
)

func TestOwnerDeactivateRequiresIdempotencyKey(t *testing.T) {
	h, mailer, _, _ := newIdentityApp(t)
	email := "deactivate-http@example.com"

	resp := callAPI(t, h, "POST", "/api/v1/owner/auth/start", map[string]string{"email": email}, "", "", "", "")
	if resp.Code != 200 {
		t.Fatalf("auth start: %d %s", resp.Code, resp.Body)
	}
	_, code := mailer.last()
	resp = callAPI(t, h, "POST", "/api/v1/owner/auth/verify", map[string]string{"email": email, "code": code}, "", "", "", "")
	if resp.Code != 200 {
		t.Fatalf("auth verify: %d %s", resp.Code, resp.Body)
	}
	session := dataMap(t, resp)
	csrf := session["csrf_token"].(string)
	var ownerCookie string
	for _, entry := range resp.Cookies {
		if entry.Name == ownerCookieName {
			ownerCookie = entry.Value
		}
	}

	resp = callAPI(t, h, "POST", "/api/v1/owner/enrollments", map[string]any{}, "", ownerCookie, csrf, "deactivate-enroll-key")
	if resp.Code != 201 {
		t.Fatalf("enrollment: %d %s", resp.Code, resp.Body)
	}
	ticket := dataMap(t, resp)["enrollment_ticket"].(string)
	resp = callAPI(t, h, "POST", "/api/v1/agent/register", map[string]any{
		"handle": "deactivatable", "enrollment_ticket": ticket,
	}, "", "", "", "deactivate-register-key")
	if resp.Code != 201 {
		t.Fatalf("register: %d %s", resp.Code, resp.Body)
	}
	agentID := dataMap(t, resp)["agent"].(map[string]any)["id"].(string)

	resp = callAPI(t, h, "DELETE", "/api/v1/owner/agents/"+agentID, nil, "", ownerCookie, csrf, "")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("deactivate without an idempotency key: %d %s", resp.Code, resp.Body)
	}

	resp = callAPI(t, h, "DELETE", "/api/v1/owner/agents/"+agentID, nil, "", ownerCookie, csrf, "deactivate-key-000001")
	if resp.Code != http.StatusOK {
		t.Fatalf("deactivate with an idempotency key: %d %s", resp.Code, resp.Body)
	}

	resp = callAPI(t, h, "DELETE", "/api/v1/owner/agents/"+agentID, nil, "", ownerCookie, csrf, "deactivate-key-000001")
	if resp.Code != http.StatusOK {
		t.Fatalf("replaying the same idempotency key must return the stored result: %d %s", resp.Code, resp.Body)
	}
}
