package platform

import (
	"encoding/base64"
	"testing"
	"time"
)

func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"PONDLY_LISTEN_ADDR", "PONDLY_ADMIN_LISTEN_ADDR", "PONDLY_MAX_BODY_BYTES",
		"PONDLY_MAX_IN_FLIGHT", "PONDLY_DATABASE_URL", "PONDLY_REDIS_URL",
		"PONDLY_MIGRATIONS_DIR", "PONDLY_PILOT_MODE", "PONDLY_COOKIE_SECURE",
		"PONDLY_CURSOR_KEY", "PONDLY_SECRET_REPLAY_KEY", "PONDLY_SMTP_HOST",
		"PONDLY_SMTP_PORT", "PONDLY_SMTP_USERNAME", "PONDLY_SMTP_PASSWORD",
		"PONDLY_SMTP_FROM", "PONDLY_OWNER_SESSION_TTL", "PONDLY_ADMIN_SESSION_TTL",
		"PONDLY_AGENT_TOKEN_TTL", "PONDLY_OTP_TTL", "PONDLY_ENROLLMENT_TTL", "PONDLY_CLAIM_TTL",
		"PONDLY_RECOVERY_TTL", "PONDLY_SECRET_REPLAY_TTL", "PONDLY_DAILY_POST_LIMIT",
		"PONDLY_DAILY_COMMENT_LIMIT", "PONDLY_DAILY_FOLLOW_LIMIT", "PONDLY_DAILY_REACTION_LIMIT",
		"PONDLY_MAX_AGENTS_PER_OWNER", "PONDLY_MAX_COMMUNITIES_PER_AGENT",
		"PONDLY_MAX_EVENT_FANOUT", "PONDLY_REDIS_POST_PER_MIN",
		"PONDLY_REDIS_COMMENT_PER_MIN", "PONDLY_REDIS_AUTH_PER_MIN",
		"PONDLY_REDIS_WRITE_PER_MIN", "PONDLY_WORKER_POLL_INTERVAL",
		"PONDLY_AUTH_START_COOLDOWN", "PONDLY_AUTH_DAILY_PER_IP",
		"PONDLY_AUTH_DAILY_PER_EMAIL",
	} {
		t.Setenv(name, "")
	}
}

func TestDefaults(t *testing.T) {
	clearConfigEnv(t)
	cfg, err := FromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddress != "127.0.0.1:8080" || cfg.MaxBodyBytes != 32768 || cfg.MaxInFlight != 64 {
		t.Fatalf("defaults: %+v", cfg)
	}
	if cfg.OwnerSessionTTL != 24*time.Hour || cfg.AdminSessionTTL != 8*time.Hour {
		t.Fatalf("session defaults: %+v", cfg)
	}
	if cfg.DailyPostLimit != 8 || cfg.DailyCommentLimit != 30 || cfg.MaxEventFanout != 100 ||
		cfg.DailyFollowLimit != 30 || cfg.DailyReactionLimit != 100 {
		t.Fatalf("quota defaults: %+v", cfg)
	}
	if cfg.AuthStartCooldown != time.Minute || cfg.RedisAuthDailyPerIP != 50 || cfg.RedisAuthDailyPerEmail != 5 {
		t.Fatalf("auth send defaults: %+v", cfg)
	}
}

func TestAuthSendLimitsFromEnvironment(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("PONDLY_AUTH_START_COOLDOWN", "30s")
	t.Setenv("PONDLY_AUTH_DAILY_PER_IP", "12")
	t.Setenv("PONDLY_AUTH_DAILY_PER_EMAIL", "3")
	cfg, err := FromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthStartCooldown != 30*time.Second || cfg.RedisAuthDailyPerIP != 12 || cfg.RedisAuthDailyPerEmail != 3 {
		t.Fatalf("auth send env: %+v", cfg)
	}
}

func TestRealModeRequiresSecretsAndSMTP(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("PONDLY_DATABASE_URL", "postgres://pondly:pondly@127.0.0.1:5432/pondly")
	t.Setenv("PONDLY_REDIS_URL", "redis://127.0.0.1:6379/0")
	if _, err := FromEnvironment(); err == nil {
		t.Fatal("expected missing cursor/replay/smtp error")
	}

	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("PONDLY_CURSOR_KEY", key)
	t.Setenv("PONDLY_SECRET_REPLAY_KEY", key)
	t.Setenv("PONDLY_SMTP_HOST", "127.0.0.1")
	t.Setenv("PONDLY_SMTP_FROM", "pondly@example.invalid")
	cfg, err := FromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.CursorKey) != 32 || len(cfg.SecretReplayKey) != 32 {
		t.Fatal("keys not decoded")
	}
	if cfg.SMTP.Port != 587 {
		t.Fatalf("smtp port = %d", cfg.SMTP.Port)
	}
}

func TestLimitValidation(t *testing.T) {
	clearConfigEnv(t)
	for _, input := range []string{"0", "abc", "9999999999999"} {
		t.Setenv("PONDLY_MAX_IN_FLIGHT", input)
		if _, err := FromEnvironment(); err == nil {
			t.Fatalf("expected error for %s", input)
		}
	}
}

func TestInvalidKeyRejected(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("PONDLY_CURSOR_KEY", "short")
	if _, err := FromEnvironment(); err == nil {
		t.Fatal("expected invalid key error")
	}
}
