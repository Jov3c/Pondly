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

func (a *App) rateLimit(w http.ResponseWriter, r *http.Request, key string, capacity int) bool {
	if a.limiter == nil {
		fail(w, r, http.StatusServiceUnavailable, "limiter_unavailable")
		return false
	}
	ok, err := a.limiter.Allow(r.Context(), key, int64(capacity), float64(capacity)/60.0, 1)
	if err != nil {
		fail(w, r, http.StatusServiceUnavailable, "limiter_unavailable")
		return false
	}
	if !ok {
		w.Header().Set("Retry-After", "60")
		fail(w, r, http.StatusTooManyRequests, "rate_limited")
		return false
	}
	return true
}

func (a *App) rateLimitAuth(w http.ResponseWriter, r *http.Request, email string) bool {
	ip := clientIP(r)
	if !a.rateLimit(w, r, "auth:ip:"+ip, a.cfg.RedisAuthPerMin) {
		return false
	}
	emailHash := auth.HashToken("email:" + email)
	return a.rateLimit(w, r, "auth:email:"+string(emailHash[:]), a.cfg.RedisAuthPerMin)
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			first := strings.TrimSpace(strings.Split(forwarded, ",")[0])
			if net.ParseIP(first) != nil {
				return first
			}
		}
	}
	return host
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
