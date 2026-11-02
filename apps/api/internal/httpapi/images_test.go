package httpapi

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestPostImagesHTTP(t *testing.T) {
	h, _, db, _ := newIdentityApp(t)
	ctx := context.Background()
	agentID, token := seedActiveAgent(t, db, "images@example.com", "imageagent")
	if _, err := db.Pool.Exec(ctx, `INSERT INTO pilot_publish_allowlist (agent_id) VALUES ($1)`, agentID); err != nil {
		t.Fatal(err)
	}

	resp := callAPI(t, h, "POST", "/api/v1/agent/posts", map[string]any{
		"post_type": "moment", "content": "",
		"image_urls": []string{"https://cdn.example.com/pond.jpg"},
	}, token, "", "", "images-post-key-0001")
	if resp.Code != 201 {
		t.Fatalf("pure image post: %d %s", resp.Code, resp.Body)
	}
	post := dataMap(t, resp)
	postID := post["id"].(string)
	images, _ := post["image_urls"].([]any)
	if len(images) != 1 || images[0] != "https://cdn.example.com/pond.jpg" || post["status"] != "published" {
		t.Fatalf("image post payload: %+v", post)
	}

	resp = callAPI(t, h, "GET", "/api/v1/public/posts/"+postID, nil, "", "", "", "")
	if resp.Code != 200 {
		t.Fatalf("public image post: %d %s", resp.Code, resp.Body)
	}
	var envelope struct {
		Data struct {
			ImageURLs     []string `json:"image_urls"`
			AuthorHandle  string   `json:"author_handle"`
			AuthorDisplay *string  `json:"author_display_name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body, &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data.ImageURLs) != 1 || envelope.Data.AuthorHandle != "imageagent" {
		t.Fatalf("public image post payload: %s", resp.Body)
	}

	for _, payload := range []map[string]any{
		{"post_type": "moment", "content": "", "image_urls": []string{"http://cdn.example.com/pond.jpg"}},
		{"post_type": "moment", "content": "", "image_urls": []string{
			"https://cdn.example.com/1.jpg", "https://cdn.example.com/2.jpg",
			"https://cdn.example.com/3.jpg", "https://cdn.example.com/4.jpg",
			"https://cdn.example.com/5.jpg",
		}},
		{"post_type": "moment", "content": ""},
	} {
		resp = callAPI(t, h, "POST", "/api/v1/agent/posts", payload, token, "", "", "images-post-"+uuid.NewString()[:20])
		if resp.Code != 422 {
			t.Fatalf("invalid images payload (%+v): %d %s", payload, resp.Code, resp.Body)
		}
	}
}
