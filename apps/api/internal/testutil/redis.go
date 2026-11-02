package testutil

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// StartRedis returns a disposable Redis URL. It uses PONDLY_TEST_REDIS_URL
// when provided, otherwise launches a local redis-server.
func StartRedis(t *testing.T) string {
	t.Helper()
	if url := os.Getenv("PONDLY_TEST_REDIS_URL"); url != "" {
		return url
	}
	bin := redisServerBinary(t)
	port := freePort(t)
	dir, err := os.MkdirTemp("", "pondly-redis-*")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin,
		"--port", fmt.Sprint(port),
		"--bind", "127.0.0.1",
		"--save", "",
		"--appendonly", "no",
		"--dir", dir,
	)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(dir)
		t.Fatalf("start redis: %v", err)
	}
	url := fmt.Sprintf("redis://127.0.0.1:%d/0", port)
	client := redis.NewClient(&redis.Options{Addr: fmt.Sprintf("127.0.0.1:%d", port)})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := client.Ping(context.Background()).Err(); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			_ = os.RemoveAll(dir)
			t.Fatal("redis did not become ready")
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = client.Close()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		_ = os.RemoveAll(dir)
	})
	return url
}

func redisServerBinary(t *testing.T) string {
	t.Helper()
	if bin := os.Getenv("PONDLY_TEST_REDIS_SERVER"); bin != "" {
		return bin
	}
	if bin, err := exec.LookPath("redis-server"); err == nil {
		return bin
	}
	if runtime.GOOS == "windows" {
		for _, candidate := range []string{
			`E:\redis\Redis-8.8.0-Windows-x64-cygwin-with-Service\redis-server.exe`,
			`C:\Program Files\Redis\redis-server.exe`,
		} {
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	t.Fatal("redis-server not found; set PONDLY_TEST_REDIS_SERVER")
	return ""
}

func RedisURLPort(t *testing.T, raw string) (host, port string) {
	t.Helper()
	cfg, err := redis.ParseURL(raw)
	if err != nil {
		t.Fatal(err)
	}
	host, port, err = net.SplitHostPort(cfg.Addr)
	if err != nil {
		t.Fatal(err)
	}
	return host, port
}

func RepoPath(rel ...string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(append([]string{filepath.Dir(file), "..", "..", "..", ".."}, rel...)...)
}
