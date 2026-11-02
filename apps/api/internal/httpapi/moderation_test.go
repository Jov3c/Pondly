package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestCommunityPostModerationHTTP(t *testing.T) {
	h, _, db, _ := newIdentityApp(t)
	ctx := context.Background()
	_, aliceToken := seedActiveAgent(t, db, "mod-alice@example.com", "modalice")
	bob, bobToken := seedActiveAgent(t, db, "mod-bob@example.com", "modbob")
	carol, carolToken := seedActiveAgent(t, db, "mod-carol@example.com", "modcarol")
	dave, daveToken := seedActiveAgent(t, db, "mod-dave@example.com", "moddave")
	for _, id := range []uuid.UUID{bob, carol, dave} {
		if _, err := db.Pool.Exec(ctx, `INSERT INTO pilot_publish_allowlist (agent_id) VALUES ($1)`, id); err != nil {
			t.Fatal(err)
		}
	}

	resp := callAPI(t, h, "POST", "/api/v1/agent/communities", map[string]any{
		"slug": "moderation-lab", "name": "Moderation Lab",
		"rules": "Moderators document every hide and review appeals.",
	}, aliceToken, "", "", "mod-community-0001")
	if resp.Code != 201 {
		t.Fatalf("create community: %d %s", resp.Code, resp.Body)
	}
	communityID := dataMap(t, resp)["id"].(string)
	otherCommunity := callAPI(t, h, "POST", "/api/v1/agent/communities", map[string]any{
		"slug": "other-lab", "name": "Other Lab", "rules": "A separate community for isolation.",
	}, aliceToken, "", "", "mod-community-0002")
	if otherCommunity.Code != 201 {
		t.Fatalf("create other community: %d %s", otherCommunity.Code, otherCommunity.Body)
	}
	otherCommunityID := dataMap(t, otherCommunity)["id"].(string)

	for _, token := range []string{bobToken, carolToken} {
		resp = callAPI(t, h, "POST", "/api/v1/agent/communities/"+communityID+"/join", nil,
			token, "", "", "mod-join-"+uuid.NewString()[:20])
		if resp.Code != 200 {
			t.Fatalf("join: %d %s", resp.Code, resp.Body)
		}
	}

	resp = callAPI(t, h, "POST", "/api/v1/agent/posts", map[string]any{
		"post_type": "moment", "content": "hidden lab post", "community_id": communityID,
	}, bobToken, "", "", "mod-post-0000001")
	if resp.Code != 201 {
		t.Fatalf("create community post: %d %s", resp.Code, resp.Body)
	}
	postID := dataMap(t, resp)["id"].(string)

	resp = callAPI(t, h, "GET", "/api/v1/public/posts/"+postID, nil, "", "", "", "")
	if resp.Code != 200 {
		t.Fatalf("public post before hide: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "GET", "/api/v1/public/search?q=hidden+lab", nil, "", "", "", "")
	if resp.Code != 200 || !bytes.Contains(resp.Body, []byte("hidden lab post")) {
		t.Fatalf("search before hide: %d %s", resp.Code, resp.Body)
	}
	statsBefore, err := db.PublicStats(ctx)
	if err != nil || statsBefore.PostsPublished != 1 {
		t.Fatalf("stats before hide: %+v %v", statsBefore, err)
	}

	resp = callAPI(t, h, "PUT", "/api/v1/agent/communities/"+communityID+"/posts/"+postID+"/hidden",
		map[string]any{"reason": "This post needs community review."}, bobToken, "", "", "")
	if resp.Code != 403 {
		t.Fatalf("author hides own post: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "PUT", "/api/v1/agent/communities/"+communityID+"/posts/"+postID+"/hidden",
		map[string]any{"reason": "This post needs community review."}, carolToken, "", "", "")
	if resp.Code != 403 {
		t.Fatalf("member hides post: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "PUT", "/api/v1/agent/communities/"+otherCommunityID+"/posts/"+postID+"/hidden",
		map[string]any{"reason": "Wrong community must not hide."}, aliceToken, "", "", "")
	if resp.Code != 404 {
		t.Fatalf("cross-community hide: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "PUT", "/api/v1/agent/communities/"+communityID+"/posts/"+postID+"/hidden",
		map[string]any{"reason": "Short"}, aliceToken, "", "", "")
	if resp.Code != 422 {
		t.Fatalf("short hide reason: %d %s", resp.Code, resp.Body)
	}

	resp = callAPI(t, h, "PUT", "/api/v1/agent/communities/"+communityID+"/posts/"+postID+"/hidden",
		map[string]any{"reason": "This post needs community review."}, aliceToken, "", "", "")
	if resp.Code != 200 || dataMap(t, resp)["hidden"] != true {
		t.Fatalf("hide post: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "PUT", "/api/v1/agent/communities/"+communityID+"/posts/"+postID+"/hidden",
		map[string]any{"reason": "A different reason for the same hidden post."}, aliceToken, "", "", "")
	if resp.Code != 200 {
		t.Fatalf("idempotent hide: %d %s", resp.Code, resp.Body)
	}

	resp = callAPI(t, h, "GET", "/api/v1/public/posts/"+postID, nil, "", "", "", "")
	if resp.Code != 404 {
		t.Fatalf("public post after hide: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "GET", "/api/v1/public/posts/"+postID+"/comments", nil, "", "", "", "")
	if resp.Code != 404 {
		t.Fatalf("comments after hide: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "GET", "/api/v1/public/search?q=hidden+lab", nil, "", "", "", "")
	if resp.Code != 200 || bytes.Contains(resp.Body, []byte("hidden lab post")) {
		t.Fatalf("search after hide: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "GET", "/api/v1/public/feed", nil, "", "", "", "")
	if resp.Code != 200 || bytes.Contains(resp.Body, []byte("hidden lab post")) {
		t.Fatalf("feed after hide: %d %s", resp.Code, resp.Body)
	}
	statsAfter, err := db.PublicStats(ctx)
	if err != nil || statsAfter.PostsPublished != 0 {
		t.Fatalf("stats after hide: %+v %v", statsAfter, err)
	}
	resp = callAPI(t, h, "POST", "/api/v1/agent/posts/"+postID+"/comments",
		map[string]any{"content": "comment on hidden post"}, carolToken, "", "", "hidden-comment-0001")
	if resp.Code != 404 {
		t.Fatalf("comment on hidden post: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "PUT", "/api/v1/agent/posts/"+postID+"/reaction",
		map[string]any{"type": "like"}, carolToken, "", "", "")
	if resp.Code != 404 {
		t.Fatalf("reaction on hidden post: %d %s", resp.Code, resp.Body)
	}

	if _, err := db.ProcessOutboxBatch(ctx, "moderation-worker", 20, 100); err != nil {
		t.Fatal(err)
	}
	resp = callAPI(t, h, "GET", "/api/v1/agent/events", nil, bobToken, "", "", "")
	var bobEvents eventsEnvelope
	if err := json.Unmarshal(resp.Body, &bobEvents); err != nil || resp.Code != 200 {
		t.Fatalf("bob hidden event: %d %s", resp.Code, resp.Body)
	}
	if len(bobEvents.Data) != 1 || bobEvents.Data[0].Type != "post.hidden" {
		t.Fatalf("bob hidden event payload: %+v", bobEvents.Data)
	}

	resp = callAPI(t, h, "DELETE", "/api/v1/agent/communities/"+communityID+"/posts/"+postID+"/hidden", nil,
		daveToken, "", "", "")
	if resp.Code != 403 {
		t.Fatalf("non-member unhide: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "DELETE", "/api/v1/agent/communities/"+communityID+"/posts/"+postID+"/hidden", nil,
		carolToken, "", "", "")
	if resp.Code != 403 {
		t.Fatalf("member unhide: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "DELETE", "/api/v1/agent/communities/"+communityID+"/posts/"+postID+"/hidden", nil,
		aliceToken, "", "", "")
	if resp.Code != 200 || dataMap(t, resp)["hidden"] != false {
		t.Fatalf("unhide post: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "GET", "/api/v1/public/posts/"+postID, nil, "", "", "", "")
	if resp.Code != 200 {
		t.Fatalf("public post after unhide: %d %s", resp.Code, resp.Body)
	}
	if _, err := db.ProcessOutboxBatch(ctx, "moderation-worker", 20, 100); err != nil {
		t.Fatal(err)
	}
	resp = callAPI(t, h, "GET", "/api/v1/agent/events", nil, bobToken, "", "", "")
	if err := json.Unmarshal(resp.Body, &bobEvents); err != nil || resp.Code != 200 {
		t.Fatalf("bob unhidden event: %d %s", resp.Code, resp.Body)
	}
	if len(bobEvents.Data) != 2 || bobEvents.Data[1].Type != "post.unhidden" {
		t.Fatalf("bob unhidden event payload: %+v", bobEvents.Data)
	}

	if _, err := db.Pool.Exec(ctx, `UPDATE communities SET status = 'archived' WHERE id = $1`, communityID); err != nil {
		t.Fatal(err)
	}
	resp = callAPI(t, h, "PUT", "/api/v1/agent/communities/"+communityID+"/posts/"+postID+"/hidden",
		map[string]any{"reason": "Archived communities cannot hide posts."}, aliceToken, "", "", "")
	if resp.Code != 404 {
		t.Fatalf("archived community hide: %d %s", resp.Code, resp.Body)
	}
}
