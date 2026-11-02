package httpapi

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Jov3c/Pondly/apps/api/internal/auth"
	"github.com/Jov3c/Pondly/apps/api/internal/store"
	"github.com/google/uuid"
)

func adminSignIn(t *testing.T, env adminEnv) (cookie, csrf string) {
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
	data := dataMap(t, resp)
	csrf = data["csrf_token"].(string)
	for _, value := range resp.Cookies {
		if value.Name == adminCookieName(false) {
			cookie = value.Value
		}
	}
	if cookie == "" || csrf == "" {
		t.Fatal("admin session incomplete")
	}
	return cookie, csrf
}

func TestPostAppealAdminHTTP(t *testing.T) {
	env := newAdminEnv(t)
	ctx := context.Background()
	alice, aliceToken := seedActiveAgent(t, env.db, "appeal-alice@example.com", "appealalice")
	bob, bobToken := seedActiveAgent(t, env.db, "appeal-bob@example.com", "appealbob")
	carol, carolToken := seedActiveAgent(t, env.db, "appeal-carol@example.com", "appealcarol")
	if _, err := env.db.Pool.Exec(ctx, `INSERT INTO pilot_publish_allowlist (agent_id) VALUES ($1)`, bob); err != nil {
		t.Fatal(err)
	}
	community, err := env.db.CreateCommunity(ctx, alice, store.CommunityInput{
		Slug: "appeal-lab", Name: "Appeal Lab",
		Rules: "Appeals are documented and reviewed by platform admins.",
	}, "agent.community.create", "appeal-community-key-0001", requestHash([]byte("appeal-community")), 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, agentID := range []uuid.UUID{bob, carol} {
		if _, err := env.db.JoinCommunity(ctx, agentID, community.ID, "agent.community.join",
			"appeal-join-"+agentID.String()[:8], requestHash([]byte("appeal-join-"+agentID.String()))); err != nil {
			t.Fatal(err)
		}
	}
	post, err := env.db.CreatePost(ctx, bob, "agent.post.create", "appeal-post-key-0001",
		requestHash([]byte("appeal-post")), store.PostInput{
			PostType: "moment", Content: "appealable community post", CommunityID: &community.ID,
		}, 10)
	if err != nil {
		t.Fatal(err)
	}

	resp := callAPI(t, env.public, "POST", "/api/v1/agent/posts/"+post.ID.String()+"/appeals",
		map[string]any{"reason": "A visible post cannot be appealed yet."}, bobToken, "", "", "appeal-key-000000")
	if resp.Code != 409 {
		t.Fatalf("appeal on visible post: %d %s", resp.Code, resp.Body)
	}

	resp = callAPI(t, env.public, "PUT",
		"/api/v1/agent/communities/"+community.ID.String()+"/posts/"+post.ID.String()+"/hidden",
		map[string]any{"reason": "Community review required for this post."}, aliceToken, "", "", "")
	if resp.Code != 200 {
		t.Fatalf("hide post: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, env.public, "GET", "/api/v1/public/posts/"+post.ID.String(), nil, "", "", "", "")
	if resp.Code != 404 {
		t.Fatalf("hidden post public: %d %s", resp.Code, resp.Body)
	}

	resp = callAPI(t, env.public, "POST", "/api/v1/agent/posts/"+post.ID.String()+"/appeals",
		map[string]any{"reason": "The post follows the community rules."}, bobToken, "", "", "appeal-key-000001")
	if resp.Code != 201 {
		t.Fatalf("create appeal: %d %s", resp.Code, resp.Body)
	}
	appealID := dataMap(t, resp)["id"].(string)
	if dataMap(t, resp)["state"] != "open" {
		t.Fatalf("appeal state: %s", resp.Body)
	}
	resp = callAPI(t, env.public, "POST", "/api/v1/agent/posts/"+post.ID.String()+"/appeals",
		map[string]any{"reason": "The post follows the community rules."}, bobToken, "", "", "appeal-key-000001")
	if resp.Code != 201 || dataMap(t, resp)["id"] != appealID {
		t.Fatalf("replay appeal: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, env.public, "POST", "/api/v1/agent/posts/"+post.ID.String()+"/appeals",
		map[string]any{"reason": "A second appeal must wait for review."}, bobToken, "", "", "appeal-key-000002")
	if resp.Code != 409 {
		t.Fatalf("duplicate open appeal: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, env.public, "POST", "/api/v1/agent/posts/"+post.ID.String()+"/appeals",
		map[string]any{"reason": "A non-author must not appeal this post."}, carolToken, "", "", "appeal-key-000003")
	if resp.Code != 404 {
		t.Fatalf("non-author appeal: %d %s", resp.Code, resp.Body)
	}

	adminCookie, csrf := adminSignIn(t, env)
	resp = callAdminAPI(t, env.admin, "GET", "/admin/v1/appeals", nil, adminCookie, "", "")
	var queue struct {
		Data []struct {
			ID               string `json:"id"`
			PostID           string `json:"post_id"`
			AppellantAgentID string `json:"appellant_agent_id"`
			Reason           string `json:"reason"`
			State            string `json:"state"`
		} `json:"data"`
		Meta struct {
			HasMore bool `json:"has_more"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(resp.Body, &queue); err != nil || resp.Code != 200 {
		t.Fatalf("admin appeal queue: %d %s", resp.Code, resp.Body)
	}
	if len(queue.Data) != 1 || queue.Data[0].ID != appealID ||
		queue.Data[0].PostID != post.ID.String() || queue.Data[0].AppellantAgentID != bob.String() ||
		queue.Data[0].State != "open" || queue.Data[0].Reason == "" {
		t.Fatalf("admin appeal queue payload: %s", resp.Body)
	}

	resp = callAdminAPI(t, env.admin, "POST", "/admin/v1/appeals/"+appealID+"/resolve",
		map[string]any{"decision": "uphold", "reason": "Community hide was justified."},
		adminCookie, "", "admin-appeal-uphold-1")
	if resp.Code != 403 {
		t.Fatalf("appeal resolve without csrf: %d %s", resp.Code, resp.Body)
	}
	resp = callAdminAPI(t, env.admin, "POST", "/admin/v1/appeals/"+appealID+"/resolve",
		map[string]any{"decision": "uphold", "reason": "Community hide was justified."},
		adminCookie, csrf, "admin-appeal-uphold-1")
	if resp.Code != 200 || dataMap(t, resp)["state"] != "upheld" {
		t.Fatalf("uphold appeal: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, env.public, "GET", "/api/v1/public/posts/"+post.ID.String(), nil, "", "", "", "")
	if resp.Code != 404 {
		t.Fatalf("upheld post must stay hidden: %d %s", resp.Code, resp.Body)
	}

	resp = callAPI(t, env.public, "POST", "/api/v1/agent/posts/"+post.ID.String()+"/appeals",
		map[string]any{"reason": "New evidence supports restoring the post."}, bobToken, "", "", "appeal-key-000004")
	if resp.Code != 201 {
		t.Fatalf("second appeal: %d %s", resp.Code, resp.Body)
	}
	secondAppealID := dataMap(t, resp)["id"].(string)
	resp = callAdminAPI(t, env.admin, "POST", "/admin/v1/appeals/"+secondAppealID+"/resolve",
		map[string]any{"decision": "overturn", "reason": "Community hide was not justified."},
		adminCookie, csrf, "admin-appeal-overturn-1")
	if resp.Code != 200 || dataMap(t, resp)["state"] != "overturned" {
		t.Fatalf("overturn appeal: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, env.public, "GET", "/api/v1/public/posts/"+post.ID.String(), nil, "", "", "", "")
	if resp.Code != 200 {
		t.Fatalf("overturned post public: %d %s", resp.Code, resp.Body)
	}

	resp = callAPI(t, env.public, "PUT",
		"/api/v1/agent/communities/"+community.ID.String()+"/posts/"+post.ID.String()+"/hidden",
		map[string]any{"reason": "A second community hide for appeal review."}, aliceToken, "", "", "")
	if resp.Code != 200 {
		t.Fatalf("second hide: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, env.public, "POST", "/api/v1/agent/posts/"+post.ID.String()+"/appeals",
		map[string]any{"reason": "The second hide should be reviewed."}, bobToken, "", "", "appeal-key-000005")
	if resp.Code != 201 {
		t.Fatalf("third appeal: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, env.public, "DELETE",
		"/api/v1/agent/communities/"+community.ID.String()+"/posts/"+post.ID.String()+"/hidden", nil,
		aliceToken, "", "", "")
	if resp.Code != 200 {
		t.Fatalf("community unhide: %d %s", resp.Code, resp.Body)
	}
	resp = callAdminAPI(t, env.admin, "GET", "/admin/v1/appeals", nil, adminCookie, "", "")
	if err := json.Unmarshal(resp.Body, &queue); err != nil || resp.Code != 200 {
		t.Fatalf("appeal queue after community unhide: %d %s", resp.Code, resp.Body)
	}
	if len(queue.Data) != 0 {
		t.Fatalf("community unhide must resolve the open appeal: %s", resp.Body)
	}

	if _, err := env.db.ProcessOutboxBatch(ctx, "appeal-worker", 50, 100); err != nil {
		t.Fatal(err)
	}
	resp = callAPI(t, env.public, "GET", "/api/v1/agent/events", nil, bobToken, "", "", "")
	var bobEvents eventsEnvelope
	if err := json.Unmarshal(resp.Body, &bobEvents); err != nil || resp.Code != 200 {
		t.Fatalf("bob moderation events: %d %s", resp.Code, resp.Body)
	}
	types := map[string]int{}
	for _, event := range bobEvents.Data {
		types[event.Type]++
	}
	if types["post.hidden"] != 2 || types["post.unhidden"] != 1 ||
		types["post.appeal.upheld"] != 1 || types["post.appeal.overturned"] != 1 {
		t.Fatalf("moderation event counts: %+v", types)
	}
}
