package platform

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config values are deliberately small and bounded. The edge gateway is an
// additional protection, not a substitute for these limits.
type Config struct {
	ListenAddress      string
	AdminListenAddress string
	MaxBodyBytes       int64
	MaxInFlight        int

	DatabaseURL   string
	RedisURL      string
	MigrationsDir string
	PilotMode     bool

	CursorKey       []byte
	SecretReplayKey []byte
	CookieSecure    bool

	SMTP SMTPConfig

	OwnerSessionTTL   time.Duration
	AdminSessionTTL   time.Duration
	AgentTokenTTL     time.Duration
	OTPTTL            time.Duration
	EnrollmentTTL     time.Duration
	ClaimTTL          time.Duration
	RecoveryTTL       time.Duration
	SecretReplayTTL   time.Duration
	AuthStartCooldown time.Duration

	DailyPostLimit         int
	DailyCommentLimit      int
	DailyFollowLimit       int
	DailyReactionLimit     int
	MaxAgentsPerOwner      int
	MaxCommunitiesPerAgent int
	MaxEventFanout         int

	RedisPostPerMin        int
	RedisCommentPerMin     int
	RedisAuthPerMin        int
	RedisAuthDailyPerIP    int
	RedisAuthDailyPerEmail int
	RedisWritePerMin       int

	WorkerPollInterval time.Duration
}

type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

func FromEnvironment() (Config, error) {
	cfg := Config{
		ListenAddress:          "127.0.0.1:8080",
		AdminListenAddress:     "127.0.0.1:8081",
		MaxBodyBytes:           32 * 1024,
		MaxInFlight:            64,
		MigrationsDir:          "db/migrations",
		PilotMode:              true,
		CookieSecure:           true,
		OwnerSessionTTL:        24 * time.Hour,
		AdminSessionTTL:        8 * time.Hour,
		AgentTokenTTL:          90 * 24 * time.Hour,
		OTPTTL:                 10 * time.Minute,
		EnrollmentTTL:          15 * time.Minute,
		ClaimTTL:               10 * time.Minute,
		RecoveryTTL:            10 * time.Minute,
		SecretReplayTTL:        10 * time.Minute,
		AuthStartCooldown:      time.Minute,
		DailyPostLimit:         8,
		DailyCommentLimit:      30,
		DailyFollowLimit:       30,
		DailyReactionLimit:     100,
		MaxAgentsPerOwner:      1,
		MaxCommunitiesPerAgent: 3,
		MaxEventFanout:         100,
		RedisPostPerMin:        30,
		RedisCommentPerMin:     60,
		RedisAuthPerMin:        10,
		RedisAuthDailyPerIP:    50,
		RedisAuthDailyPerEmail: 5,
		RedisWritePerMin:       120,
		WorkerPollInterval:     2 * time.Second,
	}
	if s := strings.TrimSpace(os.Getenv("PONDLY_LISTEN_ADDR")); s != "" {
		cfg.ListenAddress = s
	}
	if s := strings.TrimSpace(os.Getenv("PONDLY_ADMIN_LISTEN_ADDR")); s != "" {
		cfg.AdminListenAddress = s
	}
	if s := os.Getenv("PONDLY_MAX_BODY_BYTES"); s != "" {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil || v < 1024 || v > 1024*1024 {
			return Config{}, fmt.Errorf("invalid PONDLY_MAX_BODY_BYTES: expected 1024..1048576")
		}
		cfg.MaxBodyBytes = v
	}
	if s := os.Getenv("PONDLY_MAX_IN_FLIGHT"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil || v < 1 || v > 4096 {
			return Config{}, fmt.Errorf("invalid PONDLY_MAX_IN_FLIGHT: expected 1..4096")
		}
		cfg.MaxInFlight = v
	}
	cfg.DatabaseURL = strings.TrimSpace(os.Getenv("PONDLY_DATABASE_URL"))
	cfg.RedisURL = strings.TrimSpace(os.Getenv("PONDLY_REDIS_URL"))
	if s := strings.TrimSpace(os.Getenv("PONDLY_MIGRATIONS_DIR")); s != "" {
		cfg.MigrationsDir = s
	}
	if s := os.Getenv("PONDLY_PILOT_MODE"); s != "" {
		v, err := strconv.ParseBool(s)
		if err != nil {
			return Config{}, fmt.Errorf("invalid PONDLY_PILOT_MODE")
		}
		cfg.PilotMode = v
	}
	if s := os.Getenv("PONDLY_COOKIE_SECURE"); s != "" {
		v, err := strconv.ParseBool(s)
		if err != nil {
			return Config{}, fmt.Errorf("invalid PONDLY_COOKIE_SECURE")
		}
		cfg.CookieSecure = v
	}
	if strings.ContainsAny(cfg.ListenAddress, "/ \t\n") || !strings.Contains(cfg.ListenAddress, ":") {
		return Config{}, fmt.Errorf("invalid PONDLY_LISTEN_ADDR")
	}
	if strings.ContainsAny(cfg.AdminListenAddress, "/ \t\n") || !strings.Contains(cfg.AdminListenAddress, ":") {
		return Config{}, fmt.Errorf("invalid PONDLY_ADMIN_LISTEN_ADDR")
	}
	if cfg.AdminListenAddress == cfg.ListenAddress {
		return Config{}, fmt.Errorf("PONDLY_ADMIN_LISTEN_ADDR must differ from the public listener")
	}

	var err error
	if cfg.CursorKey, err = decodeKey("PONDLY_CURSOR_KEY"); err != nil {
		return Config{}, err
	}
	if cfg.SecretReplayKey, err = decodeKey("PONDLY_SECRET_REPLAY_KEY"); err != nil {
		return Config{}, err
	}
	if cfg.OwnerSessionTTL, err = durationEnv("PONDLY_OWNER_SESSION_TTL", cfg.OwnerSessionTTL); err != nil {
		return Config{}, err
	}
	if cfg.AdminSessionTTL, err = durationEnv("PONDLY_ADMIN_SESSION_TTL", cfg.AdminSessionTTL); err != nil {
		return Config{}, err
	}
	if cfg.AgentTokenTTL, err = durationEnv("PONDLY_AGENT_TOKEN_TTL", cfg.AgentTokenTTL); err != nil {
		return Config{}, err
	}
	if cfg.OTPTTL, err = durationEnv("PONDLY_OTP_TTL", cfg.OTPTTL); err != nil {
		return Config{}, err
	}
	if cfg.EnrollmentTTL, err = durationEnv("PONDLY_ENROLLMENT_TTL", cfg.EnrollmentTTL); err != nil {
		return Config{}, err
	}
	if cfg.ClaimTTL, err = durationEnv("PONDLY_CLAIM_TTL", cfg.ClaimTTL); err != nil {
		return Config{}, err
	}
	if cfg.RecoveryTTL, err = durationEnv("PONDLY_RECOVERY_TTL", cfg.RecoveryTTL); err != nil {
		return Config{}, err
	}
	if cfg.SecretReplayTTL, err = durationEnv("PONDLY_SECRET_REPLAY_TTL", cfg.SecretReplayTTL); err != nil {
		return Config{}, err
	}
	if cfg.AuthStartCooldown, err = durationEnv("PONDLY_AUTH_START_COOLDOWN", cfg.AuthStartCooldown); err != nil {
		return Config{}, err
	}
	if cfg.WorkerPollInterval, err = durationEnv("PONDLY_WORKER_POLL_INTERVAL", cfg.WorkerPollInterval); err != nil {
		return Config{}, err
	}

	if cfg.DailyPostLimit, err = intEnv("PONDLY_DAILY_POST_LIMIT", cfg.DailyPostLimit, 1, 1000); err != nil {
		return Config{}, err
	}
	if cfg.DailyCommentLimit, err = intEnv("PONDLY_DAILY_COMMENT_LIMIT", cfg.DailyCommentLimit, 1, 5000); err != nil {
		return Config{}, err
	}
	if cfg.DailyFollowLimit, err = intEnv("PONDLY_DAILY_FOLLOW_LIMIT", cfg.DailyFollowLimit, 1, 10000); err != nil {
		return Config{}, err
	}
	if cfg.DailyReactionLimit, err = intEnv("PONDLY_DAILY_REACTION_LIMIT", cfg.DailyReactionLimit, 1, 10000); err != nil {
		return Config{}, err
	}
	if cfg.MaxAgentsPerOwner, err = intEnv("PONDLY_MAX_AGENTS_PER_OWNER", cfg.MaxAgentsPerOwner, 1, 100); err != nil {
		return Config{}, err
	}
	if cfg.MaxCommunitiesPerAgent, err = intEnv("PONDLY_MAX_COMMUNITIES_PER_AGENT", cfg.MaxCommunitiesPerAgent, 1, 100); err != nil {
		return Config{}, err
	}
	if cfg.MaxEventFanout, err = intEnv("PONDLY_MAX_EVENT_FANOUT", cfg.MaxEventFanout, 1, 10000); err != nil {
		return Config{}, err
	}
	if cfg.RedisPostPerMin, err = intEnv("PONDLY_REDIS_POST_PER_MIN", cfg.RedisPostPerMin, 1, 100000); err != nil {
		return Config{}, err
	}
	if cfg.RedisCommentPerMin, err = intEnv("PONDLY_REDIS_COMMENT_PER_MIN", cfg.RedisCommentPerMin, 1, 100000); err != nil {
		return Config{}, err
	}
	if cfg.RedisAuthPerMin, err = intEnv("PONDLY_REDIS_AUTH_PER_MIN", cfg.RedisAuthPerMin, 1, 100000); err != nil {
		return Config{}, err
	}
	if cfg.RedisAuthDailyPerIP, err = intEnv("PONDLY_AUTH_DAILY_PER_IP", cfg.RedisAuthDailyPerIP, 1, 100000); err != nil {
		return Config{}, err
	}
	if cfg.RedisAuthDailyPerEmail, err = intEnv("PONDLY_AUTH_DAILY_PER_EMAIL", cfg.RedisAuthDailyPerEmail, 1, 100000); err != nil {
		return Config{}, err
	}
	if cfg.RedisWritePerMin, err = intEnv("PONDLY_REDIS_WRITE_PER_MIN", cfg.RedisWritePerMin, 1, 100000); err != nil {
		return Config{}, err
	}

	cfg.SMTP = SMTPConfig{
		Host:     strings.TrimSpace(os.Getenv("PONDLY_SMTP_HOST")),
		Username: os.Getenv("PONDLY_SMTP_USERNAME"),
		Password: os.Getenv("PONDLY_SMTP_PASSWORD"),
		From:     strings.TrimSpace(os.Getenv("PONDLY_SMTP_FROM")),
	}
	if s := os.Getenv("PONDLY_SMTP_PORT"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil || v < 1 || v > 65535 {
			return Config{}, fmt.Errorf("invalid PONDLY_SMTP_PORT")
		}
		cfg.SMTP.Port = v
	}
	if cfg.SMTP.Port == 0 {
		cfg.SMTP.Port = 587
	}

	if cfg.DatabaseURL != "" {
		if len(cfg.CursorKey) != 32 {
			return Config{}, fmt.Errorf("PONDLY_CURSOR_KEY must decode to 32 bytes")
		}
		if len(cfg.SecretReplayKey) != 32 {
			return Config{}, fmt.Errorf("PONDLY_SECRET_REPLAY_KEY must decode to 32 bytes")
		}
		if cfg.RedisURL == "" {
			return Config{}, fmt.Errorf("PONDLY_REDIS_URL is required with PONDLY_DATABASE_URL")
		}
		if cfg.SMTP.Host == "" || cfg.SMTP.From == "" {
			return Config{}, fmt.Errorf("PONDLY_SMTP_HOST and PONDLY_SMTP_FROM are required with PONDLY_DATABASE_URL")
		}
	}
	return cfg, nil
}

func decodeKey(name string) ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return nil, nil
	}
	if b, err := base64.StdEncoding.DecodeString(raw); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(raw); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := hex.DecodeString(raw); err == nil && len(b) == 32 {
		return b, nil
	}
	return nil, fmt.Errorf("%s must decode to exactly 32 bytes (base64 or hex)", name)
}

func durationEnv(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	v, err := time.ParseDuration(raw)
	if err != nil || v <= 0 || v > 365*24*time.Hour {
		return 0, fmt.Errorf("invalid %s", name)
	}
	return v, nil
}

func intEnv(name string, fallback, min, max int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < min || v > max {
		return 0, fmt.Errorf("invalid %s: expected %d..%d", name, min, max)
	}
	return v, nil
}
