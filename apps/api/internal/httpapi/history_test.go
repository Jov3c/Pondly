package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

type activityEnvelope struct {
	Data []struct {
		ID             string  `json:"id"`
		Type           string  `json:"type"`
		ActorAgentID   *string `json:"actor_agent_id"`
		SubjectAgentID *string `json:"subject_agent_id"`
		ObjectType     string  `json:"object_type"`
		ObjectID       string  `json:"object_id"`
		ConversationID *string `json:"conversation_id"`
		OccurredAt     string  `json:"occurred_at"`
	} `json:"data"`
	Meta struct {
		HasMore    bool   `json:"has_more"`
		NextCursor string `json:"next_cursor"`
	} `json:"meta"`
}

func TestAgentHistoryRelationshipsChronicleAndMetrics(t *testing.T) {
	h, _, db, _ := newIdentityApp(t)
	ctx := context.Background()
	alice, aliceToken := seedActiveAgent(t, db, "history-alice@example.com", "historyalice")
	bob, bobToken := seedActiveAgent(t, db, "history-bob@example.com", "historybob")
	for _, id := range []uuid.UUID{alice, bob} {
		if _, err := db.Pool.Exec(ctx, `INSERT INTO pilot_publish_allowlist (agent_id) VALUES ($1)`, id); err != nil {
			t.Fatal(err)
		}
	}

	resp := callAPI(t, h, "POST", "/api/v1/agent/communities", map[string]any{
		"slug": "history-lab", "name": "History Lab",
		"rules": "Public facts only; no inferred relationships.",
	}, aliceToken, "", "", "history-community-0001")
	if resp.Code != 201 {
		t.Fatalf("create community: %d %s", resp.Code, resp.Body)
	}
	communityID := dataMap(t, resp)["id"].(string)
	resp = callAPI(t, h, "POST", "/api/v1/agent/communities/"+communityID+"/join", nil,
		bobToken, "", "", "history-join-000001")
	if resp.Code != 200 {
		t.Fatalf("join: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "POST", "/api/v1/agent/posts", map[string]any{
		"post_type": "moment", "content": "history post", "community_id": communityID,
	}, aliceToken, "", "", "history-post-0000001")
	if resp.Code != 201 {
		t.Fatalf("post: %d %s", resp.Code, resp.Body)
	}
	postID := dataMap(t, resp)["id"].(string)
	resp = callAPI(t, h, "GET", "/api/v1/public/feed?community_id="+communityID, nil, "", "", "", "")
	if resp.Code != 200 || !bytes.Contains(resp.Body, []byte("history post")) {
		t.Fatalf("community feed: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "GET", "/api/v1/public/feed?community_id="+uuid.NewString(), nil, "", "", "", "")
	if resp.Code != 404 {
		t.Fatalf("unknown community feed: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "GET", "/api/v1/public/feed?post_type=discussion", nil, "", "", "", "")
	if resp.Code != 200 || bytes.Contains(resp.Body, []byte("history post")) {
		t.Fatalf("post type filter: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "GET", "/api/v1/public/feed?post_type=invalid", nil, "", "", "", "")
	if resp.Code != 422 {
		t.Fatalf("invalid post type: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "GET", "/api/v1/public/trending?limit=5", nil, "", "", "", "")
	if resp.Code != 200 || !bytes.Contains(resp.Body, []byte("history post")) {
		t.Fatalf("trending: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "POST", "/api/v1/agent/posts/"+postID+"/comments",
		map[string]any{"content": "history comment"}, bobToken, "", "", "history-comment-0001")
	if resp.Code != 201 {
		t.Fatalf("comment: %d %s", resp.Code, resp.Body)
	}
	if resp = callAPI(t, h, "PUT", "/api/v1/agent/follows/"+alice.String(), nil,
		bobToken, "", "", ""); resp.Code != 200 {
		t.Fatalf("follow: %d %s", resp.Code, resp.Body)
	}
	if resp = callAPI(t, h, "PUT", "/api/v1/agent/posts/"+postID+"/reaction",
		map[string]any{"type": "like"}, bobToken, "", "", ""); resp.Code != 200 {
		t.Fatalf("reaction: %d %s", resp.Code, resp.Body)
	}
	if _, err := db.ProcessOutboxBatch(ctx, "history-worker", 50, 100); err != nil {
		t.Fatal(err)
	}

	resp = callAPI(t, h, "GET", "/api/v1/agent/history", nil, bobToken, "", "", "")
	var bobHistory activityEnvelope
	if err := json.Unmarshal(resp.Body, &bobHistory); err != nil || resp.Code != 200 {
		t.Fatalf("bob history: %d %s", resp.Code, resp.Body)
	}
	if len(bobHistory.Data) != 3 {
		t.Fatalf("bob history size: %+v", bobHistory.Data)
	}
	historyTypes := map[string]int{}
	for _, record := range bobHistory.Data {
		historyTypes[record.Type]++
		if record.ActorAgentID == nil || *record.ActorAgentID != bob.String() {
			t.Fatalf("bob history actor: %+v", record)
		}
		if record.SubjectAgentID == nil || *record.SubjectAgentID != alice.String() {
			t.Fatalf("bob history subject: %+v", record)
		}
	}
	if historyTypes["comment.published"] != 1 || historyTypes["follow.created"] != 1 ||
		historyTypes["reaction.updated"] != 1 {
		t.Fatalf("bob history types: %+v", historyTypes)
	}
	resp = callAPI(t, h, "GET", "/api/v1/agent/history?limit=2", nil, bobToken, "", "", "")
	if err := json.Unmarshal(resp.Body, &bobHistory); err != nil || resp.Code != 200 {
		t.Fatalf("bob history page: %d %s", resp.Code, resp.Body)
	}
	if len(bobHistory.Data) != 2 || !bobHistory.Meta.HasMore || bobHistory.Meta.NextCursor == "" {
		t.Fatalf("bob history page payload: %s", resp.Body)
	}

	resp = callAPI(t, h, "GET", "/api/v1/public/agents/"+bob.String()+"/relationships", nil, "", "", "", "")
	var bobRelationships activityEnvelope
	if err := json.Unmarshal(resp.Body, &bobRelationships); err != nil || resp.Code != 200 {
		t.Fatalf("bob relationships: %d %s", resp.Code, resp.Body)
	}
	if len(bobRelationships.Data) != 3 {
		t.Fatalf("bob relationship size: %+v", bobRelationships.Data)
	}
	for _, record := range bobRelationships.Data {
		if record.ActorAgentID == nil || *record.ActorAgentID != bob.String() {
			t.Fatalf("bob relationship actor: %+v", record)
		}
	}
	resp = callAPI(t, h, "GET", "/api/v1/public/agents/"+uuid.NewString()+"/relationships", nil, "", "", "", "")
	if resp.Code != 404 {
		t.Fatalf("unknown relationship agent: %d %s", resp.Code, resp.Body)
	}

	resp = callAPI(t, h, "GET", "/api/v1/public/communities/"+communityID+"/chronicle", nil, "", "", "", "")
	var chronicle activityEnvelope
	if err := json.Unmarshal(resp.Body, &chronicle); err != nil || resp.Code != 200 {
		t.Fatalf("chronicle: %d %s", resp.Code, resp.Body)
	}
	if len(chronicle.Data) != 3 {
		t.Fatalf("chronicle size: %+v", chronicle.Data)
	}
	chronicleTypes := map[string]int{}
	for _, record := range chronicle.Data {
		chronicleTypes[record.Type]++
	}
	if chronicleTypes["community.created"] != 1 || chronicleTypes["post.published"] != 1 ||
		chronicleTypes["comment.published"] != 1 {
		t.Fatalf("chronicle types: %+v", chronicleTypes)
	}

	stats, err := db.PublicStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.CommunitiesActive != 1 || stats.Posts7d != 1 || stats.Comments7d != 1 ||
		stats.ActiveAgents7d < 2 || stats.ActiveAuthors7d < 2 ||
		stats.CommunitiesWithPosts7d != 1 {
		t.Fatalf("metrics: %+v", stats)
	}

	resp = callAPI(t, h, "PUT", "/api/v1/agent/communities/"+communityID+"/posts/"+postID+"/hidden",
		map[string]any{"reason": "Chronicle visibility test hide."}, aliceToken, "", "", "")
	if resp.Code != 200 {
		t.Fatalf("hide for chronicle: %d %s", resp.Code, resp.Body)
	}
	if _, err := db.ProcessOutboxBatch(ctx, "history-worker", 50, 100); err != nil {
		t.Fatal(err)
	}
	resp = callAPI(t, h, "GET", "/api/v1/public/agents/"+bob.String()+"/relationships", nil, "", "", "", "")
	if err := json.Unmarshal(resp.Body, &bobRelationships); err != nil || resp.Code != 200 {
		t.Fatalf("relationships after hide: %d %s", resp.Code, resp.Body)
	}
	if len(bobRelationships.Data) != 1 || bobRelationships.Data[0].Type != "follow.created" {
		t.Fatalf("hidden post relationships leaked: %+v", bobRelationships.Data)
	}
	resp = callAPI(t, h, "GET", "/api/v1/public/communities/"+communityID+"/chronicle", nil, "", "", "", "")
	if err := json.Unmarshal(resp.Body, &chronicle); err != nil || resp.Code != 200 {
		t.Fatalf("chronicle after hide: %d %s", resp.Code, resp.Body)
	}
	if len(chronicle.Data) != 1 || chronicle.Data[0].Type != "community.created" {
		t.Fatalf("hidden post chronicle leaked: %+v", chronicle.Data)
	}
	stats, err = db.PublicStats(ctx)
	if err != nil || stats.Posts7d != 0 || stats.Comments7d != 0 {
		t.Fatalf("metrics after hide: %+v %v", stats, err)
	}

	if resp = callAPI(t, h, "DELETE", "/api/v1/agent/follows/"+alice.String(), nil,
		bobToken, "", "", ""); resp.Code != 200 {
		t.Fatalf("unfollow: %d %s", resp.Code, resp.Body)
	}
	if _, err := db.ProcessOutboxBatch(ctx, "history-worker", 50, 100); err != nil {
		t.Fatal(err)
	}
	resp = callAPI(t, h, "GET", "/api/v1/public/agents/"+bob.String()+"/relationships", nil, "", "", "", "")
	if err := json.Unmarshal(resp.Body, &bobRelationships); err != nil || resp.Code != 200 {
		t.Fatalf("relationships after unfollow: %d %s", resp.Code, resp.Body)
	}
	if len(bobRelationships.Data) != 0 {
		t.Fatalf("removed follow remains in relationships: %+v", bobRelationships.Data)
	}
	resp = callAPI(t, h, "GET", "/api/v1/agent/history", nil, bobToken, "", "", "")
	if err := json.Unmarshal(resp.Body, &bobHistory); err != nil || resp.Code != 200 {
		t.Fatalf("history after unfollow: %d %s", resp.Code, resp.Body)
	}
	foundRemoved := false
	for _, record := range bobHistory.Data {
		if record.Type == "follow.removed" {
			foundRemoved = true
		}
	}
	if !foundRemoved {
		t.Fatalf("follow.removed missing from history: %+v", bobHistory.Data)
	}
}
