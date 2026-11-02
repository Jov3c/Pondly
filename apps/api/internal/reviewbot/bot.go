package reviewbot

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/Jov3c/Pondly/apps/api/internal/store"
	"github.com/google/uuid"
)

type Stats struct {
	Approved int
	Rejected int
	Held     int
	Skipped  int
	Errors   int
}

type ReviewStore interface {
	ListBotReviewItems(ctx context.Context, limit int) ([]store.BotReviewItem, error)
	SystemHold(ctx context.Context, targetType string, targetID uuid.UUID, reason string) error
	SystemDecidePost(ctx context.Context, postID uuid.UUID, decision, reason string) (store.Post, error)
	SystemDecideComment(ctx context.Context, commentID uuid.UUID, decision, reason string) (store.Comment, error)
}

type Bot struct {
	Store  ReviewStore
	Logger *log.Logger
	Batch  int
}

func New(store ReviewStore, logger *log.Logger) *Bot {
	if logger == nil {
		logger = log.Default()
	}
	return &Bot{Store: store, Logger: logger, Batch: 50}
}

func (b *Bot) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			b.RunOnce(ctx)
			timer.Reset(interval)
		}
	}
}

func (b *Bot) RunOnce(ctx context.Context) Stats {
	var stats Stats
	if b.Store == nil {
		b.Logger.Printf("review bot: store unavailable")
		stats.Errors++
		return stats
	}
	items, err := b.Store.ListBotReviewItems(ctx, b.Batch)
	if err != nil {
		b.Logger.Printf("review bot: list pending: %v", err)
		stats.Errors++
		return stats
	}
	for _, item := range items {
		var decision Decision
		switch item.Kind {
		case "post":
			title := ""
			if item.Title != nil {
				title = *item.Title
			}
			decision = ReviewPost(title, item.Content, item.ImageCount)
		case "comment":
			decision = ReviewComment(item.Content)
		default:
			b.Logger.Printf("review bot: unknown item kind %q", item.Kind)
			stats.Errors++
			continue
		}
		if item.AuthorStatus != "active" {
			decision = Decision{Action: Hold, Reason: "bot:hold:author_inactive"}
		}
		if decision.Action == Hold {
			reason := auditReason(decision)
			if err := b.Store.SystemHold(ctx, item.Kind, item.ID, reason); err != nil {
				b.Logger.Printf("review bot: hold %s %s: %v", item.Kind, item.ID, err)
				stats.Errors++
				continue
			}
			stats.Held++
			continue
		}
		reason := auditReason(decision)
		var decideErr error
		switch item.Kind {
		case "post":
			if decision.Action == Approve {
				_, decideErr = b.Store.SystemDecidePost(ctx, item.ID, "approve", reason)
			} else {
				_, decideErr = b.Store.SystemDecidePost(ctx, item.ID, "reject", reason)
			}
		case "comment":
			if decision.Action == Approve {
				_, decideErr = b.Store.SystemDecideComment(ctx, item.ID, "approve", reason)
			} else {
				_, decideErr = b.Store.SystemDecideComment(ctx, item.ID, "reject", reason)
			}
		}
		if errors.Is(decideErr, store.ErrConflict) {
			stats.Skipped++
			continue
		}
		if decideErr != nil {
			b.Logger.Printf("review bot: decide %s %s: %v", item.Kind, item.ID, decideErr)
			stats.Errors++
			continue
		}
		if decision.Action == Approve {
			stats.Approved++
		} else {
			stats.Rejected++
		}
		b.Logger.Printf("review bot: %s %s %s reason=%s", item.Kind, item.ID, decision.Action, reason)
	}
	return stats
}

func auditReason(decision Decision) string {
	return decision.Reason + "@" + RuleVersion
}
