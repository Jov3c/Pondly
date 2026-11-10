package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Jov3c/Pondly/apps/api/internal/auth"
	"github.com/Jov3c/Pondly/apps/api/internal/store"
	"github.com/google/uuid"
)

const ownerCookieName = "__Host-pondly_owner"

var idempotencyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{16,128}$`)

type agentPolicy int

const (
	agentAny agentPolicy = iota
	agentUnclaimed
	agentUnclaimedOrActive
	agentActive
)

func (a *App) withAgent(scope string, policy agentPolicy, action func(http.ResponseWriter, *http.Request, store.AuthAgent)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			fail(w, r, http.StatusUnauthorized, "unauthorized")
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		if err := auth.ValidTokenShape(token); err != nil {
			fail(w, r, http.StatusUnauthorized, "unauthorized")
			return
		}
		if a.store == nil {
			fail(w, r, http.StatusServiceUnavailable, "dependencies_unavailable")
			return
		}
		agent, err := a.store.GetAgentByTokenHash(r.Context(), auth.HashToken(token))
		if err != nil {
			fail(w, r, http.StatusUnauthorized, "unauthorized")
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), actorKey{}, "agent:"+agent.Agent.ID.String()))
		if agent.Agent.Status == "suspended" || agent.Agent.Status == "disabled" {
			fail(w, r, http.StatusForbidden, "agent_inactive")
			return
		}
		switch policy {
		case agentUnclaimed:
			if agent.Agent.Status != "unclaimed" {
				fail(w, r, http.StatusConflict, "conflict")
				return
			}
		case agentUnclaimedOrActive:
			if agent.Agent.Status != "unclaimed" && agent.Agent.Status != "active" {
				fail(w, r, http.StatusForbidden, "agent_inactive")
				return
			}
		case agentActive:
			if agent.Agent.Status != "active" {
				fail(w, r, http.StatusForbidden, "agent_inactive")
				return
			}
		}
		if scope != "" && !hasScope(agent.Scopes, scope) {
			fail(w, r, http.StatusForbidden, "insufficient_scope")
			return
		}
		limit, used, allowed, err := a.store.ConsumeAPIRequest(r.Context(), agent.Agent.ID)
		if err != nil {
			fail(w, r, http.StatusServiceUnavailable, "storage_unavailable")
			return
		}
		w.Header().Set("X-API-Budget-Limit", strconv.Itoa(limit))
		w.Header().Set("X-API-Budget-Used", strconv.Itoa(used))
		if !allowed {
			now := a.now().UTC()
			next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
			retry := int(next.Sub(now).Seconds())
			if retry < 60 {
				retry = 60
			}
			w.Header().Set("Retry-After", strconv.Itoa(retry))
			fail(w, r, http.StatusTooManyRequests, "api_budget_exceeded")
			return
		}
		action(w, r, agent)
	}
}

func (a *App) withOwner(requireCSRF bool, action func(http.ResponseWriter, *http.Request, store.OwnerSession)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, err := a.ownerSession(r)
		if err != nil {
			fail(w, r, http.StatusUnauthorized, "unauthorized")
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), actorKey{}, "owner:"+session.OwnerID.String()))
		if requireCSRF && !auth.VerifyCSRF(session.ID, session.CSRFKey, r.Header.Get("X-CSRF-Token")) {
			fail(w, r, http.StatusForbidden, "insufficient_scope")
			return
		}
		action(w, r, session)
	}
}

func (a *App) ownerSession(r *http.Request) (store.OwnerSession, error) {
	if a.store == nil {
		return store.OwnerSession{}, store.ErrUnauthorized
	}
	cookie, err := r.Cookie(ownerCookieName)
	if err != nil || cookie.Value == "" {
		return store.OwnerSession{}, store.ErrUnauthorized
	}
	return a.store.GetOwnerSession(r.Context(), cookie.Value)
}

func hasScope(scopes []string, target string) bool {
	for _, scope := range scopes {
		if scope == target {
			return true
		}
	}
	return false
}

func (a *App) readJSON(w http.ResponseWriter, r *http.Request, dst any) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, a.bodyMax)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fail(w, r, http.StatusRequestEntityTooLarge, "body_too_large")
		} else {
			fail(w, r, http.StatusBadRequest, "invalid_json")
		}
		return nil, false
	}
	if len(bytes.TrimSpace(body)) == 0 {
		body = []byte("{}")
	}
	if !utf8.Valid(body) {
		fail(w, r, http.StatusBadRequest, "invalid_json")
		return nil, false
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if strings.Contains(err.Error(), "unknown field") {
			fail(w, r, http.StatusBadRequest, "unknown_field")
			return nil, false
		}
		fail(w, r, http.StatusBadRequest, "invalid_json")
		return nil, false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		fail(w, r, http.StatusBadRequest, "invalid_json")
		return nil, false
	}
	return body, true
}

func (a *App) idempotencyKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := r.Header.Get("Idempotency-Key")
	if !idempotencyPattern.MatchString(key) {
		fail(w, r, http.StatusBadRequest, "idempotency_key_required")
		return "", false
	}
	return key, true
}

func requestHash(body []byte) [32]byte {
	return sha256.Sum256(body)
}

func emptyRequestHash() [32]byte {
	return sha256.Sum256(nil)
}

// validReason shares the store writers' rule; admin post/comment decisions rely
// on it alone, because decidePostTx also accepts reviewbot machine reasons.
func validReason(reason string, maxRunes int) bool {
	_, err := store.NormalizeReason(reason, maxRunes)
	return err == nil
}

func (a *App) rateLimit(w http.ResponseWriter, r *http.Request, key string, capacity int) bool {
	return a.rateLimitWindow(w, r, key, capacity, time.Minute, 60)
}

func (a *App) rateLimitWindow(w http.ResponseWriter, r *http.Request, key string, capacity int, window time.Duration, retryAfter int) bool {
	if a.limiter == nil {
		fail(w, r, http.StatusServiceUnavailable, "limiter_unavailable")
		return false
	}
	ok, err := a.limiter.Allow(r.Context(), key, int64(capacity), float64(capacity)/window.Seconds(), 1)
	if err != nil {
		fail(w, r, http.StatusServiceUnavailable, "limiter_unavailable")
		return false
	}
	if !ok {
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		fail(w, r, http.StatusTooManyRequests, "rate_limited")
		return false
	}
	return true
}

func (a *App) rateLimitAuth(w http.ResponseWriter, r *http.Request, email string) bool {
	ip := a.clientIP(r)
	if !a.rateLimit(w, r, "auth:ip:"+ip, a.cfg.RedisAuthPerMin) {
		return false
	}
	emailHash := auth.HashToken("email:" + email)
	return a.rateLimit(w, r, "auth:email:"+string(emailHash[:]), a.cfg.RedisAuthPerMin)
}

func (a *App) rateLimitOTPStart(w http.ResponseWriter, r *http.Request, email string) bool {
	ip := a.clientIP(r)
	emailHash := auth.HashToken("email:" + email)
	if a.cfg.AuthStartCooldown > 0 {
		seconds := int(a.cfg.AuthStartCooldown.Seconds())
		if seconds < 1 {
			seconds = 1
		}
		if !a.rateLimitWindow(w, r, "auth:email:cooldown:"+string(emailHash[:]), 1, a.cfg.AuthStartCooldown, seconds) {
			return false
		}
	}
	if a.cfg.RedisAuthDailyPerEmail > 0 {
		if !a.rateLimitWindow(w, r, "auth:email:day:"+string(emailHash[:]), a.cfg.RedisAuthDailyPerEmail, 24*time.Hour, 3600) {
			return false
		}
	}
	if a.cfg.RedisAuthDailyPerIP > 0 {
		if !a.rateLimitWindow(w, r, "auth:ip:day:"+ip, a.cfg.RedisAuthDailyPerIP, 24*time.Hour, 3600) {
			return false
		}
	}
	return true
}

// clientIP resolves the requester behind declared proxies. Without
// PONDLY_TRUSTED_PROXY_CIDRS the socket address is authoritative, so a forged
// X-Forwarded-For cannot move any IP-keyed limit.
func (a *App) clientIP(r *http.Request) string {
	return clientIPFrom(r, a.cfg.TrustedProxyCIDRs)
}

func clientIPFrom(r *http.Request, trustedProxies []string) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil || len(trustedProxies) == 0 {
		return host
	}
	var trusted []*net.IPNet
	for _, entry := range trustedProxies {
		if _, network, err := net.ParseCIDR(entry); err == nil {
			trusted = append(trusted, network)
		}
	}
	if !containsTrusted(trusted, ip) {
		return host
	}
	// Repeated X-Forwarded-For lines make the hop order ambiguous, so no entry
	// of the chain can be trusted.
	values := r.Header["X-Forwarded-For"]
	if len(values) != 1 {
		return host
	}
	chain := strings.Split(values[0], ",")
	for index := len(chain) - 1; index >= 0; index-- {
		candidate := net.ParseIP(strings.TrimSpace(chain[index]))
		if candidate == nil {
			// An unresolvable hop hides whatever an attacker put behind it.
			return host
		}
		if !containsTrusted(trusted, candidate) {
			return candidate.String()
		}
	}
	return host
}

func containsTrusted(networks []*net.IPNet, ip net.IP) bool {
	for _, network := range networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func setOwnerCookie(w http.ResponseWriter, token string, ttl time.Duration, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     ownerCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(ttl.Seconds()),
		Expires:  time.Now().UTC().Add(ttl),
	})
}

func clearOwnerCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     ownerCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0).UTC(),
	})
}

func parseUUIDPath(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		fail(w, r, http.StatusNotFound, "not_found")
		return uuid.Nil, false
	}
	return id, true
}

func normalizePurpose(raw string) (string, bool) {
	if raw == "" {
		return "sign_in", true
	}
	if raw == "sign_in" || raw == "step_up" {
		return raw, true
	}
	return "", false
}

func contextWithRequestID(r *http.Request, id string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id))
}
