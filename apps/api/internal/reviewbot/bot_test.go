package reviewbot

import (
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"testing"

	"github.com/Jov3c/Pondly/apps/api/internal/store"
	"github.com/google/uuid"
)

type fakeStore struct {
	items       []store.BotReviewItem
	postCalls   []string
	commentCall []string
	holdCalls   []string
	failIDs     map[uuid.UUID]error
}

func (f *fakeStore) ListBotReviewItems(context.Context, int) ([]store.BotReviewItem, error) {
	return f.items, nil
}

func (f *fakeStore) SystemHold(_ context.Context, targetType string, targetID uuid.UUID, reason string) error {
	f.holdCalls = append(f.holdCalls, targetType+":"+reason)
	return nil
}

func (f *fakeStore) SystemDecidePost(_ context.Context, postID uuid.UUID, decision, reason string) (store.Post, error) {
	f.postCalls = append(f.postCalls, decision+":"+reason)
	if err := f.failIDs[postID]; err != nil {
		return store.Post{}, err
	}
	return store.Post{ID: postID, Status: "published"}, nil
}

func (f *fakeStore) SystemDecideComment(_ context.Context, commentID uuid.UUID, decision, reason string) (store.Comment, error) {
	f.commentCall = append(f.commentCall, decision+":"+reason)
	if err := f.failIDs[commentID]; err != nil {
		return store.Comment{}, err
	}
	return store.Comment{ID: commentID, Status: "published"}, nil
}

func TestRunOnceAppliesRuleDecisions(t *testing.T) {
	safePost := store.BotReviewItem{Kind: "post", ID: uuid.New(), Content: "记录今天的判断和下一步。", AuthorStatus: "active"}
	linkComment := store.BotReviewItem{Kind: "comment", ID: uuid.New(), Content: "完整来源 https://example.com/note", AuthorStatus: "active"}
	spamPost := store.BotReviewItem{Kind: "post", ID: uuid.New(), Content: "兼职日结，扫码领取", AuthorStatus: "active"}
	safeComment := store.BotReviewItem{Kind: "comment", ID: uuid.New(), Content: "我先复述对方最后一句。", AuthorStatus: "active"}
	htmlPost := store.BotReviewItem{Kind: "post", ID: uuid.New(), Content: "<div>需要人工看</div>", AuthorStatus: "active"}
	conflictPost := store.BotReviewItem{Kind: "post", ID: uuid.New(), Content: "和人工审核并发。", AuthorStatus: "active"}
	failingComment := store.BotReviewItem{Kind: "comment", ID: uuid.New(), Content: "写入失败。", AuthorStatus: "active"}
	unknown := store.BotReviewItem{Kind: "reaction", ID: uuid.New(), Content: "x", AuthorStatus: "active"}
	suspended := store.BotReviewItem{Kind: "comment", ID: uuid.New(), Content: "安全内容但作者已暂停。", AuthorStatus: "suspended"}

	fake := &fakeStore{
		items: []store.BotReviewItem{safePost, linkComment, spamPost, safeComment, htmlPost, conflictPost, failingComment, unknown, suspended},
		failIDs: map[uuid.UUID]error{
			conflictPost.ID:   store.ErrConflict,
			failingComment.ID: errors.New("database unavailable"),
		},
	}
	bot := New(fake, log.New(io.Discard, "", 0))
	stats := bot.RunOnce(context.Background())
	if stats.Approved != 2 || stats.Rejected != 1 || stats.Held != 3 || stats.Skipped != 1 || stats.Errors != 2 {
		t.Fatalf("stats: %+v", stats)
	}
	if len(fake.postCalls) != 3 || len(fake.commentCall) != 2 || len(fake.holdCalls) != 3 {
		t.Fatalf("calls: posts=%v comments=%v holds=%v", fake.postCalls, fake.commentCall, fake.holdCalls)
	}
	if !strings.HasPrefix(fake.postCalls[0], "approve:bot:approve:safe@") {
		t.Fatalf("safe post call: %v", fake.postCalls)
	}
	if !strings.Contains(fake.postCalls[1], "reject:bot:reject:blocked_phrase@") {
		t.Fatalf("spam post call: %v", fake.postCalls)
	}
	if !strings.Contains(fake.commentCall[0], "approve:bot:approve:safe@") {
		t.Fatalf("safe comment call: %v", fake.commentCall)
	}
}
