package ratelimit

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Jov3c/Pondly/apps/api/internal/testutil"
)

func TestTokenBucketAndRefill(t *testing.T) {
	ctx := context.Background()
	limiter, err := NewFromURL(ctx, testutil.StartRedis(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = limiter.Close() })
	key := "test:" + t.Name() + ":" + strconv.FormatInt(time.Now().UnixNano(), 10)

	for i := 0; i < 3; i++ {
		ok, err := limiter.Allow(ctx, key, 3, 0.1, 1)
		if err != nil || !ok {
			t.Fatalf("call %d: ok=%v err=%v", i, ok, err)
		}
	}
	if ok, err := limiter.Allow(ctx, key, 3, 0.1, 1); err != nil || ok {
		t.Fatalf("expected denied, ok=%v err=%v", ok, err)
	}

	refillKey := "test:refill:" + t.Name() + ":" + strconv.FormatInt(time.Now().UnixNano(), 10)
	ok, err := limiter.Allow(ctx, refillKey, 2, 20, 1)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	time.Sleep(150 * time.Millisecond)
	if ok, err := limiter.Allow(ctx, refillKey, 2, 20, 1); err != nil || !ok {
		t.Fatalf("expected refill, ok=%v err=%v", ok, err)
	}
}

func TestConcurrentAllowIsAtomic(t *testing.T) {
	ctx := context.Background()
	limiter, err := NewFromURL(ctx, testutil.StartRedis(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = limiter.Close() })
	key := "test:concurrent:" + t.Name() + ":" + strconv.FormatInt(time.Now().UnixNano(), 10)

	var allowed atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := limiter.Allow(ctx, key, 10, 0.001, 1)
			if err != nil {
				t.Errorf("allow: %v", err)
				return
			}
			if ok {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := allowed.Load(); got != 10 {
		t.Fatalf("allowed = %d, want 10", got)
	}
}

func TestUnavailableRedisIsAnError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := NewFromURL(ctx, "redis://127.0.0.1:1/0"); err == nil {
		t.Fatal("expected connection error")
	}
	var missing *Limiter
	if ok, err := missing.Allow(context.Background(), "x", 1, 1, 1); err == nil || ok {
		t.Fatal("nil limiter must fail closed")
	}
}
