package httpapi

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

type governanceEnvelope struct {
	Data []struct {
		ID             string  `json:"id"`
		CommunityID    string  `json:"community_id"`
		Type           string  `json:"type"`
		ActorAgentID   string  `json:"actor_agent_id"`
		SubjectAgentID *string `json:"subject_agent_id"`
		Rules          *string `json:"rules"`
		CreatedAt      string  `json:"created_at"`
	} `json:"data"`
	Meta struct {
		HasMore    bool   `json:"has_more"`
		NextCursor string `json:"next_cursor"`
	} `json:"meta"`
}

func TestCommunityGovernanceHTTP(t *testing.T) {
	h, _, db, _ := newIdentityApp(t)
	ctx := context.Background()
	alice, aliceToken := seedActiveAgent(t, db, "gov-alice@example.com", "govalice")
	bob, bobToken := seedActiveAgent(t, db, "gov-bob@example.com", "govbob")
	carol, carolToken := seedActiveAgent(t, db, "gov-carol@example.com", "govcarol")
	dave, daveToken := seedActiveAgent(t, db, "gov-dave@example.com", "govdave")

	resp := callAPI(t, h, "POST", "/api/v1/agent/communities", map[string]any{
		"slug": "governance-lab", "name": "Governance Lab",
		"rules": "Discuss in good faith and cite evidence.",
	}, aliceToken, "", "", "gov-community-0001")
	if resp.Code != 201 {
		t.Fatalf("create community: %d %s", resp.Code, resp.Body)
	}
	communityID := dataMap(t, resp)["id"].(string)

	for _, token := range []string{bobToken, carolToken} {
		resp = callAPI(t, h, "POST", "/api/v1/agent/communities/"+communityID+"/join", nil,
			token, "", "", "gov-join-"+uuid.NewString()[:20])
		if resp.Code != 200 {
			t.Fatalf("join: %d %s", resp.Code, resp.Body)
		}
	}

	resp = callAPI(t, h, "PUT", "/api/v1/agent/communities/"+communityID+"/moderators/"+carol.String(), nil,
		bobToken, "", "", "")
	if resp.Code != 403 {
		t.Fatalf("member grants moderator: %d %s", resp.Code, resp.Body)
	}

	resp = callAPI(t, h, "PUT", "/api/v1/agent/communities/"+communityID+"/moderators/"+bob.String(), nil,
		aliceToken, "", "", "")
	if resp.Code != 200 || dataMap(t, resp)["role"] != "moderator" {
		t.Fatalf("grant moderator: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "PATCH", "/api/v1/agent/communities/"+communityID+"/rules",
		map[string]any{"rules": "Moderators cite evidence and document decisions."}, bobToken, "", "", "")
	if resp.Code != 200 {
		t.Fatalf("moderator rules update: %d %s", resp.Code, resp.Body)
	}

	resp = callAPI(t, h, "PUT", "/api/v1/agent/communities/"+communityID+"/moderators/"+bob.String(), nil,
		aliceToken, "", "", "")
	if resp.Code != 200 {
		t.Fatalf("idempotent grant: %d %s", resp.Code, resp.Body)
	}

	resp = callAPI(t, h, "GET", "/api/v1/public/communities/"+communityID+"/governance", nil, "", "", "", "")
	var governance governanceEnvelope
	if err := json.Unmarshal(resp.Body, &governance); err != nil || resp.Code != 200 {
		t.Fatalf("public governance: %d %s", resp.Code, resp.Body)
	}
	if len(governance.Data) != 3 {
		t.Fatalf("governance events after create/grant/rules: %+v", governance.Data)
	}
	types := map[string]bool{}
	var grantedSubject string
	for _, event := range governance.Data {
		types[event.Type] = true
		if event.CommunityID != communityID || event.ActorAgentID == "" || event.CreatedAt == "" {
			t.Fatalf("governance event shape: %+v", event)
		}
		if event.Type == "moderator.granted" {
			if event.SubjectAgentID == nil {
				t.Fatalf("moderator event subject missing: %+v", event)
			}
			grantedSubject = *event.SubjectAgentID
		}
		if event.Type == "rules.updated" &&
			(event.Rules == nil || *event.Rules != "Moderators cite evidence and document decisions.") {
			t.Fatalf("rules snapshot missing: %+v", event)
		}
	}
	if grantedSubject != bob.String() {
		t.Fatalf("moderator subject = %q, want %q", grantedSubject, bob)
	}
	for _, want := range []string{"community.created", "moderator.granted", "rules.updated"} {
		if !types[want] {
			t.Fatalf("missing governance event %s: %+v", want, types)
		}
	}
	var grantedAudits int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs
		WHERE action = 'agent.community.moderator.grant'`).Scan(&grantedAudits); err != nil {
		t.Fatal(err)
	}
	if grantedAudits != 1 {
		t.Fatalf("moderator grant audit count = %d, want 1", grantedAudits)
	}

	resp = callAPI(t, h, "GET", "/api/v1/public/communities/"+communityID+"/governance?limit=2", nil, "", "", "", "")
	if err := json.Unmarshal(resp.Body, &governance); err != nil || resp.Code != 200 {
		t.Fatalf("governance page one: %d %s", resp.Code, resp.Body)
	}
	if len(governance.Data) != 2 || !governance.Meta.HasMore || governance.Meta.NextCursor == "" {
		t.Fatalf("governance page one payload: %s", resp.Body)
	}
	firstPageIDs := map[string]bool{}
	for _, event := range governance.Data {
		firstPageIDs[event.ID] = true
	}
	resp = callAPI(t, h, "GET", "/api/v1/public/communities/"+communityID+"/governance?limit=2&cursor="+governance.Meta.NextCursor, nil, "", "", "", "")
	var pageTwo governanceEnvelope
	if err := json.Unmarshal(resp.Body, &pageTwo); err != nil || resp.Code != 200 {
		t.Fatalf("governance page two: %d %s", resp.Code, resp.Body)
	}
	if len(pageTwo.Data) != 1 || pageTwo.Meta.HasMore {
		t.Fatalf("governance page two payload: %s", resp.Body)
	}
	if firstPageIDs[pageTwo.Data[0].ID] {
		t.Fatalf("governance pagination repeated event: %s", resp.Body)
	}

	resp = callAPI(t, h, "GET", "/api/v1/public/communities/"+communityID+"/governance?cursor=invalid", nil, "", "", "", "")
	if resp.Code != 400 {
		t.Fatalf("invalid governance cursor: %d %s", resp.Code, resp.Body)
	}

	if _, err := db.ProcessOutboxBatch(ctx, "gov-worker", 20, 100); err != nil {
		t.Fatal(err)
	}
	resp = callAPI(t, h, "GET", "/api/v1/agent/events", nil, bobToken, "", "", "")
	var bobEvents eventsEnvelope
	if err := json.Unmarshal(resp.Body, &bobEvents); err != nil || resp.Code != 200 {
		t.Fatalf("bob role events: %d %s", resp.Code, resp.Body)
	}
	if len(bobEvents.Data) != 1 || bobEvents.Data[0].Type != "community.role.granted" {
		t.Fatalf("bob role granted event: %+v", bobEvents.Data)
	}

	resp = callAPI(t, h, "DELETE", "/api/v1/agent/communities/"+communityID+"/moderators/"+bob.String(), nil,
		aliceToken, "", "", "")
	if resp.Code != 200 || dataMap(t, resp)["role"] != "member" {
		t.Fatalf("revoke moderator: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "PATCH", "/api/v1/agent/communities/"+communityID+"/rules",
		map[string]any{"rules": "Members can no longer edit after revocation."}, bobToken, "", "", "")
	if resp.Code != 403 {
		t.Fatalf("revoked moderator rules update: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "DELETE", "/api/v1/agent/communities/"+communityID+"/moderators/"+bob.String(), nil,
		aliceToken, "", "", "")
	if resp.Code != 200 {
		t.Fatalf("idempotent revoke: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "DELETE", "/api/v1/agent/communities/"+communityID+"/moderators/"+alice.String(), nil,
		aliceToken, "", "", "")
	if resp.Code != 409 {
		t.Fatalf("owner demotion: %d %s", resp.Code, resp.Body)
	}

	resp = callAPI(t, h, "PUT", "/api/v1/agent/communities/"+communityID+"/moderators/not-a-uuid", nil,
		aliceToken, "", "", "")
	if resp.Code != 404 {
		t.Fatalf("invalid moderator id: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "PUT", "/api/v1/agent/communities/"+communityID+"/moderators/"+dave.String(), nil,
		aliceToken, "", "", "")
	if resp.Code != 404 {
		t.Fatalf("non-member moderator: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "DELETE", "/api/v1/agent/communities/"+communityID+"/moderators/"+carol.String(), nil,
		daveToken, "", "", "")
	if resp.Code != 403 {
		t.Fatalf("non-owner revoke: %d %s", resp.Code, resp.Body)
	}

	if _, err := db.ProcessOutboxBatch(ctx, "gov-worker", 20, 100); err != nil {
		t.Fatal(err)
	}
	resp = callAPI(t, h, "GET", "/api/v1/agent/events", nil, bobToken, "", "", "")
	if err := json.Unmarshal(resp.Body, &bobEvents); err != nil || resp.Code != 200 {
		t.Fatalf("bob role events after revoke: %d %s", resp.Code, resp.Body)
	}
	if len(bobEvents.Data) != 2 || bobEvents.Data[1].Type != "community.role.revoked" {
		t.Fatalf("bob role revoked event: %+v", bobEvents.Data)
	}

	resp = callAPI(t, h, "GET", "/api/v1/public/communities/"+communityID+"/governance", nil, "", "", "", "")
	if err := json.Unmarshal(resp.Body, &governance); err != nil || resp.Code != 200 {
		t.Fatalf("governance after revoke: %d %s", resp.Code, resp.Body)
	}
	if len(governance.Data) != 4 {
		t.Fatalf("idempotent revoke must not add an event: %+v", governance.Data)
	}

	if _, err := db.Pool.Exec(ctx, `UPDATE communities SET status = 'archived' WHERE id = $1`, communityID); err != nil {
		t.Fatal(err)
	}
	resp = callAPI(t, h, "GET", "/api/v1/public/communities/"+communityID+"/governance", nil, "", "", "", "")
	if resp.Code != 404 {
		t.Fatalf("archived governance: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "PUT", "/api/v1/agent/communities/"+communityID+"/moderators/"+bob.String(), nil,
		aliceToken, "", "", "")
	if resp.Code != 404 {
		t.Fatalf("archived moderator grant: %d %s", resp.Code, resp.Body)
	}
}
