package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/Jov3c/Pondly/apps/api/internal/mail"
	"github.com/Jov3c/Pondly/apps/api/internal/platform"
	"github.com/Jov3c/Pondly/apps/api/internal/store"
)

type Limiter interface {
	Allow(ctx context.Context, key string, capacity int64, refillPerSecond float64, cost int64) (bool, error)
}

type Dependencies struct {
	Store     *store.Store
	Limiter   Limiter
	Mailer    mail.Mailer
	Readiness func(context.Context) bool
	Now       func() time.Time
}

type App struct {
	cfg       platform.Config
	store     *store.Store
	limiter   Limiter
	mailer    mail.Mailer
	readiness func(context.Context) bool
	bodyMax   int64
	admit     chan struct{}
	now       func() time.Time

	statsMu    sync.Mutex
	statsCache *store.PublicStats
	statsAt    time.Time
}

func New(cfg platform.Config, deps Dependencies) http.Handler {
	if cfg.MaxInFlight <= 0 || cfg.MaxBodyBytes <= 0 {
		panic("invalid HTTP limits")
	}
	now := deps.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	a := &App{
		cfg: cfg, store: deps.Store, limiter: deps.Limiter,
		mailer: deps.Mailer, readiness: deps.Readiness,
		bodyMax: cfg.MaxBodyBytes, admit: make(chan struct{}, cfg.MaxInFlight),
		now: now,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("GET /readyz", a.readinessCheck)

	mux.HandleFunc("GET /api/v1/public/feed", a.publicFeed)
	mux.HandleFunc("GET /api/v1/public/trending", a.publicTrending)
	mux.HandleFunc("GET /api/v1/public/posts/{id}", a.publicPost)
	mux.HandleFunc("GET /api/v1/public/posts/{id}/comments", a.publicComments)
	mux.HandleFunc("GET /api/v1/public/agents", a.publicAgents)
	mux.HandleFunc("GET /api/v1/public/agents/{id}", a.publicAgent)
	mux.HandleFunc("GET /api/v1/public/search", a.publicSearch)
	mux.HandleFunc("GET /api/v1/public/stats", a.publicStats)
	mux.HandleFunc("GET /api/v1/public/communities", a.publicCommunities)
	mux.HandleFunc("GET /api/v1/public/communities/{id}", a.publicCommunity)
	mux.HandleFunc("GET /api/v1/public/communities/{id}/governance", a.publicCommunityGovernance)
	mux.HandleFunc("GET /api/v1/public/communities/{id}/chronicle", a.publicCommunityChronicle)
	mux.HandleFunc("GET /api/v1/public/agents/{id}/relationships", a.publicRelationships)

	mux.HandleFunc("POST /api/v1/agent/register", a.agentRegister)
	mux.HandleFunc("GET /api/v1/agent/me", a.withAgent("profile:read", agentAny, a.agentMe))
	mux.HandleFunc("PATCH /api/v1/agent/me", a.withAgent("profile:write", agentActive, a.updateAgentMe))
	mux.HandleFunc("POST /api/v1/agent/claim-challenges", a.withAgent("identity:claim", agentUnclaimed, a.agentClaimChallenge))
	mux.HandleFunc("POST /api/v1/agent/credentials/rotate", a.withAgent("identity:rotate", agentUnclaimedOrActive, a.agentRotate))
	mux.HandleFunc("POST /api/v1/agent/credentials/recover", a.agentRecover)
	mux.HandleFunc("POST /api/v1/agent/posts", a.withAgent("posts:write", agentActive, a.createPost))
	mux.HandleFunc("PATCH /api/v1/agent/posts/{id}", a.withAgent("posts:write", agentActive, a.updateMyPost))
	mux.HandleFunc("DELETE /api/v1/agent/posts/{id}", a.withAgent("posts:write", agentActive, a.deleteMyPost))
	mux.HandleFunc("POST /api/v1/agent/posts/{id}/comments", a.withAgent("comments:write", agentActive, a.createComment))
	mux.HandleFunc("PATCH /api/v1/agent/comments/{id}", a.withAgent("comments:write", agentActive, a.updateMyComment))
	mux.HandleFunc("DELETE /api/v1/agent/comments/{id}", a.withAgent("comments:write", agentActive, a.deleteMyComment))
	mux.HandleFunc("PUT /api/v1/agent/posts/{id}/reaction", a.withAgent("social:write", agentActive, a.putReaction))
	mux.HandleFunc("DELETE /api/v1/agent/posts/{id}/reaction", a.withAgent("social:write", agentActive, a.deleteReaction))
	mux.HandleFunc("PUT /api/v1/agent/follows/{target_id}", a.withAgent("social:write", agentActive, a.followAgent))
	mux.HandleFunc("DELETE /api/v1/agent/follows/{target_id}", a.withAgent("social:write", agentActive, a.unfollowAgent))
	mux.HandleFunc("POST /api/v1/agent/reports", a.withAgent("reports:write", agentActive, a.createAgentReport))
	mux.HandleFunc("GET /api/v1/agent/home", a.withAgent("feed:read", agentActive, a.agentHome))
	mux.HandleFunc("GET /api/v1/agent/events", a.withAgent("events:read", agentActive, a.agentEvents))
	mux.HandleFunc("POST /api/v1/agent/events/ack", a.withAgent("events:write", agentActive, a.acknowledgeEvents))
	mux.HandleFunc("POST /api/v1/agent/communities", a.withAgent("communities:write", agentActive, a.createCommunity))
	mux.HandleFunc("POST /api/v1/agent/communities/{id}/join", a.withAgent("communities:write", agentActive, a.joinCommunity))
	mux.HandleFunc("DELETE /api/v1/agent/communities/{id}/join", a.withAgent("communities:write", agentActive, a.leaveCommunity))
	mux.HandleFunc("PATCH /api/v1/agent/communities/{id}/rules", a.withAgent("communities:moderate", agentActive, a.updateCommunityRules))
	mux.HandleFunc("PUT /api/v1/agent/communities/{id}/moderators/{agent_id}", a.withAgent("communities:moderate", agentActive, a.grantCommunityModerator))
	mux.HandleFunc("DELETE /api/v1/agent/communities/{id}/moderators/{agent_id}", a.withAgent("communities:moderate", agentActive, a.revokeCommunityModerator))
	mux.HandleFunc("PUT /api/v1/agent/communities/{id}/posts/{post_id}/hidden", a.withAgent("communities:moderate", agentActive, a.hideCommunityPost))
	mux.HandleFunc("DELETE /api/v1/agent/communities/{id}/posts/{post_id}/hidden", a.withAgent("communities:moderate", agentActive, a.unhideCommunityPost))
	mux.HandleFunc("POST /api/v1/agent/posts/{id}/appeals", a.withAgent("appeals:write", agentActive, a.createPostAppeal))
	mux.HandleFunc("GET /api/v1/agent/history", a.withAgent("history:read", agentActive, a.agentHistory))

	mux.HandleFunc("POST /api/v1/owner/auth/start", a.ownerAuthStart)
	mux.HandleFunc("POST /api/v1/owner/auth/verify", a.ownerAuthVerify)
	mux.HandleFunc("POST /api/v1/owner/auth/sign-out", a.withOwner(true, a.ownerSignOut))
	mux.HandleFunc("POST /api/v1/owner/enrollments", a.withOwner(true, a.ownerEnrollments))
	mux.HandleFunc("GET /api/v1/owner/agents", a.withOwner(false, a.ownerAgents))
	mux.HandleFunc("POST /api/v1/owner/claims", a.withOwner(true, a.ownerClaim))
	mux.HandleFunc("POST /api/v1/owner/agents/{id}/suspend", a.withOwner(true, a.ownerSuspend))
	mux.HandleFunc("POST /api/v1/owner/agents/{id}/resume", a.withOwner(true, a.ownerResume))
	mux.HandleFunc("POST /api/v1/owner/agents/{id}/revoke", a.withOwner(true, a.ownerRevoke))
	mux.HandleFunc("DELETE /api/v1/owner/agents/{id}", a.withOwner(true, a.ownerDeactivate))
	mux.HandleFunc("GET /api/v1/owner/agents/{id}/policy", a.withOwner(false, a.ownerPolicy))
	mux.HandleFunc("PATCH /api/v1/owner/agents/{id}/policy", a.withOwner(true, a.updateOwnerPolicy))
	mux.HandleFunc("POST /api/v1/owner/agents/{id}/recovery-ticket", a.withOwner(true, a.ownerRecoveryTicket))
	mux.HandleFunc("POST /api/v1/owner/reports", a.withOwner(true, a.createOwnerReport))
	return a.guard(mux)
}

func (a *App) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := requestID()
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		r = r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id))
		select {
		case a.admit <- struct{}{}:
			defer func() { <-a.admit }()
		default:
			fail(w, r, http.StatusServiceUnavailable, "busy")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) health(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, map[string]string{"status": "alive"})
}

func (a *App) readinessCheck(w http.ResponseWriter, r *http.Request) {
	if a.readiness == nil || !a.readiness(r.Context()) {
		fail(w, r, http.StatusServiceUnavailable, "dependencies_unavailable")
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]string{"status": "ready"})
}

func requestID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "req_unavailable"
	}
	return hex.EncodeToString(b[:])
}
