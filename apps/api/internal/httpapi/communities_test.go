package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
)

type communityListEnvelope struct {
	Data []struct {
		ID   string `json:"id"`
		Slug string `json:"slug"`
	} `json:"data"`
	Meta struct {
		HasMore    bool   `json:"has_more"`
		NextCursor string `json:"next_cursor"`
	} `json:"meta"`
}

func TestCommunityHTTPFlow(t *testing.T) {
	h, _, db, _ := newIdentityApp(t)
	ctx := context.Background()
	_, aliceToken := seedActiveAgent(t, db, "community-alice@example.com", "commalice")
	_, bobToken := seedActiveAgent(t, db, "community-bob@example.com", "commbob")
	_, carolToken := seedActiveAgent(t, db, "community-carol@example.com", "commcarol")

	resp := callAPI(t, h, "POST", "/api/v1/agent/communities", map[string]any{
		"slug": "agent-lab", "name": "Agent Lab", "description": "Notes from the lab",
		"rules": "Cite evidence and stay kind.",
	}, aliceToken, "", "", "community-create-0001")
	if resp.Code != 201 {
		t.Fatalf("create community: %d %s", resp.Code, resp.Body)
	}
	community := dataMap(t, resp)
	communityID := community["id"].(string)
	if communityID == "" || community["slug"] != "agent-lab" || community["rules"] != "Cite evidence and stay kind." {
		t.Fatalf("community payload: %+v", community)
	}

	resp = callAPI(t, h, "POST", "/api/v1/agent/communities", map[string]any{
		"slug": "agent-lab", "name": "Other Lab", "rules": "A different set of rules.",
	}, bobToken, "", "", "community-create-0002")
	if resp.Code != 409 {
		t.Fatalf("duplicate slug: %d %s", resp.Code, resp.Body)
	}

	resp = callAPI(t, h, "POST", "/api/v1/agent/communities", map[string]any{
		"slug": "Bad Slug", "name": "Bad", "rules": "short",
	}, aliceToken, "", "", "community-create-0003")
	if resp.Code != 422 {
		t.Fatalf("invalid community: %d %s", resp.Code, resp.Body)
	}

	resp = callAPI(t, h, "POST", "/api/v1/agent/communities/"+communityID+"/join", nil,
		bobToken, "", "", "community-join-000001")
	if resp.Code != 200 {
		t.Fatalf("join community: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "POST", "/api/v1/agent/communities/"+communityID+"/join", nil,
		bobToken, "", "", "community-join-000002")
	if resp.Code != 200 {
		t.Fatalf("join community twice: %d %s", resp.Code, resp.Body)
	}

	resp = callAPI(t, h, "POST", "/api/v1/agent/posts", map[string]any{
		"post_type": "moment", "content": "bob posts inside the lab", "community_id": communityID,
	}, bobToken, "", "", "community-post-000001")
	if resp.Code != 201 {
		t.Fatalf("community post: %d %s", resp.Code, resp.Body)
	}
	post := dataMap(t, resp)
	if post["community_id"] != communityID {
		t.Fatalf("post community: %+v", post)
	}

	resp = callAPI(t, h, "POST", "/api/v1/agent/posts", map[string]any{
		"post_type": "moment", "content": "carol tries without joining", "community_id": communityID,
	}, carolToken, "", "", "community-post-000002")
	if resp.Code != 403 {
		t.Fatalf("non-member post: %d %s", resp.Code, resp.Body)
	}
	var errBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(resp.Body, &errBody); err != nil || errBody.Error.Code != "not_member" {
		t.Fatalf("non-member error: %s", resp.Body)
	}

	resp = callAPI(t, h, "POST", "/api/v1/agent/posts", map[string]any{
		"post_type": "moment", "content": "bob targets a missing lab",
		"community_id": uuid.NewString(),
	}, bobToken, "", "", "community-post-000003")
	if resp.Code != 404 {
		t.Fatalf("missing community post: %d %s", resp.Code, resp.Body)
	}

	resp = callAPI(t, h, "PATCH", "/api/v1/agent/communities/"+communityID+"/rules",
		map[string]any{"rules": "Only members may rewrite these rules."}, bobToken, "", "", "")
	if resp.Code != 403 {
		t.Fatalf("member rules update: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "PATCH", "/api/v1/agent/communities/"+communityID+"/rules",
		map[string]any{"rules": "Cite evidence, stay kind, and sign your work."}, aliceToken, "", "", "")
	if resp.Code != 200 {
		t.Fatalf("owner rules update: %d %s", resp.Code, resp.Body)
	}
	if dataMap(t, resp)["rules"] != "Cite evidence, stay kind, and sign your work." {
		t.Fatalf("rules not updated: %s", resp.Body)
	}

	resp = callAPI(t, h, "GET", "/api/v1/public/communities/"+communityID, nil, "", "", "", "")
	if resp.Code != 200 {
		t.Fatalf("public community: %d %s", resp.Code, resp.Body)
	}
	if dataMap(t, resp)["slug"] != "agent-lab" {
		t.Fatalf("public community payload: %s", resp.Body)
	}
	resp = callAPI(t, h, "GET", "/api/v1/public/communities", nil, "", "", "", "")
	var firstPage communityListEnvelope
	if err := json.Unmarshal(resp.Body, &firstPage); err != nil || resp.Code != 200 {
		t.Fatalf("public communities: %d %s", resp.Code, resp.Body)
	}
	if len(firstPage.Data) != 1 || firstPage.Data[0].ID != communityID {
		t.Fatalf("public communities payload: %s", resp.Body)
	}

	for i, slug := range []string{"agent-lab-two", "agent-lab-three"} {
		resp = callAPI(t, h, "POST", "/api/v1/agent/communities", map[string]any{
			"slug": slug, "name": "Agent Lab " + slug, "rules": "Rules for another laboratory.",
		}, aliceToken, "", "", fmt.Sprintf("community-create-extra-%d", i))
		if resp.Code != 201 {
			t.Fatalf("extra community %s: %d %s", slug, resp.Code, resp.Body)
		}
	}
	resp = callAPI(t, h, "POST", "/api/v1/agent/communities", map[string]any{
		"slug": "agent-lab-four", "name": "Agent Lab Four", "rules": "Rules for a fourth laboratory.",
	}, aliceToken, "", "", "community-create-extra-d")
	if resp.Code != 409 {
		t.Fatalf("community cap: %d %s", resp.Code, resp.Body)
	}

	resp = callAPI(t, h, "GET", "/api/v1/public/communities?cursor=not-a-cursor", nil, "", "", "", "")
	if resp.Code != 400 || !bytes.Contains(resp.Body, []byte("invalid_cursor")) {
		t.Fatalf("invalid community cursor: %d %s", resp.Code, resp.Body)
	}

	resp = callAPI(t, h, "GET", "/api/v1/public/communities?limit=2", nil, "", "", "", "")
	if err := json.Unmarshal(resp.Body, &firstPage); err != nil || resp.Code != 200 {
		t.Fatalf("public page one: %d %s", resp.Code, resp.Body)
	}
	if len(firstPage.Data) != 2 || !firstPage.Meta.HasMore || firstPage.Meta.NextCursor == "" {
		t.Fatalf("public page one payload: %s", resp.Body)
	}
	resp = callAPI(t, h, "GET", "/api/v1/public/communities?limit=2&cursor="+firstPage.Meta.NextCursor, nil, "", "", "", "")
	var secondPage communityListEnvelope
	if err := json.Unmarshal(resp.Body, &secondPage); err != nil || resp.Code != 200 {
		t.Fatalf("public page two: %d %s", resp.Code, resp.Body)
	}
	if len(secondPage.Data) != 1 || secondPage.Meta.HasMore || secondPage.Meta.NextCursor != "" {
		t.Fatalf("public page two payload: %s", resp.Body)
	}

	if _, err := db.Pool.Exec(ctx, `UPDATE communities SET status = 'archived' WHERE id = $1`, communityID); err != nil {
		t.Fatal(err)
	}
	resp = callAPI(t, h, "GET", "/api/v1/public/communities/"+communityID, nil, "", "", "", "")
	if resp.Code != 404 {
		t.Fatalf("archived public community: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "POST", "/api/v1/agent/posts", map[string]any{
		"post_type": "moment", "content": "bob posts into an archived lab", "community_id": communityID,
	}, bobToken, "", "", "community-post-000004")
	if resp.Code != 404 {
		t.Fatalf("archived community post: %d %s", resp.Code, resp.Body)
	}
}
