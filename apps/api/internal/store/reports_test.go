package store

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestReportsQueueAndResolution(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	agentID, _, ownerID := createActiveAgent(t, s, "report-owner@example.com", "reporter")
	adminID, _, _, _ := createAdmin(t, s)
	post, err := s.CreatePost(ctx, agentID, "agent.post.create", idemKey(), requestHash("report-post"),
		PostInput{PostType: "moment", Content: "report target"}, 10)
	if err != nil {
		t.Fatal(err)
	}

	key := idemKey()
	report, err := s.CreateAgentReport(ctx, agentID, "post", post.ID,
		"this post violates the pilot rules", "agent.report.create", key, requestHash("report-agent"))
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.CreateAgentReport(ctx, agentID, "post", post.ID,
		"this post violates the pilot rules", "agent.report.create", key, requestHash("report-agent"))
	if err != nil || replay.ID != report.ID {
		t.Fatalf("report replay: %+v %v", replay, err)
	}
	ownerReport, err := s.CreateOwnerReport(ctx, ownerID, "post", post.ID,
		"owner safety report for this post", "owner.report.create", idemKey(), requestHash("report-owner"))
	if err != nil {
		t.Fatal(err)
	}
	if ownerReport.ID == uuid.Nil {
		t.Fatal("owner report missing id")
	}

	cases, _, hasMore, err := s.ListOpenReports(ctx, "", 20)
	if err != nil || len(cases) != 2 || hasMore {
		t.Fatalf("open reports: %+v more=%v err=%v", cases, hasMore, err)
	}
	resolved, err := s.ResolveReport(ctx, adminID, report.ID, "resolve",
		"reviewed and resolved", "admin.report.resolve", idemKey(), requestHash("resolve"))
	if err != nil || resolved.State != "resolved" {
		t.Fatalf("resolve: %+v %v", resolved, err)
	}
	if _, err := s.ResolveReport(ctx, adminID, report.ID, "dismiss",
		"second decision", "admin.report.resolve", idemKey(), requestHash("resolve-2")); !errors.Is(err, ErrConflict) {
		t.Fatalf("second decision: %v", err)
	}
	cases, _, _, err = s.ListOpenReports(ctx, "", 20)
	if err != nil || len(cases) != 1 || cases[0].ID != ownerReport.ID {
		t.Fatalf("queue after resolve: %+v %v", cases, err)
	}
	if _, err := s.CreateAgentReport(ctx, agentID, "post", uuid.Nil,
		"missing target report", "agent.report.create", idemKey(), requestHash("missing")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing target: %v", err)
	}
	if _, err := s.CreateAgentReport(ctx, agentID, "community", post.ID,
		"community report", "agent.report.create", idemKey(), requestHash("community")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("community target: %v", err)
	}
	if _, err := s.CreateAgentReport(ctx, agentID, "post", post.ID,
		"short", "agent.report.create", idemKey(), requestHash("short")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("short report: %v", err)
	}
}
