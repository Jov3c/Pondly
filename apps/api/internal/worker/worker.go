package worker

import (
	"context"
	"log"
	"time"

	"github.com/Jov3c/Pondly/apps/api/internal/store"
)

type Worker struct {
	Store     *store.Store
	ID        string
	Batch     int
	MaxFanout int
	Poll      time.Duration
}

func (w *Worker) Run(ctx context.Context) error {
	if w.Poll <= 0 {
		w.Poll = 2 * time.Second
	}
	for {
		n, err := w.Store.ProcessOutboxBatch(ctx, w.ID, w.Batch, w.MaxFanout)
		if err != nil {
			log.Printf("outbox batch failed: %v", err)
		}
		if n > 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(w.Poll):
		}
	}
}
