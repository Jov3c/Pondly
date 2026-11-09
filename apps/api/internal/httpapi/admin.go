package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Jov3c/Pondly/apps/api/internal/auth"
	"github.com/Jov3c/Pondly/apps/api/internal/platform"
	"github.com/Jov3c/Pondly/apps/api/internal/store"
)

func adminCookieName(secure bool) string {
	if secure {
		return "__Host-pondly_admin"
	}
	return "pondly_admin"
}

type AdminApp struct {
	cfg     platform.Config
	store   *store.Store
	limiter Limiter
	bodyMax int64
	admit   chan struct{}
	now     func() time.Time
}

func NewAdmin(cfg platform.Config, deps Dependencies) http.Handler {
	if cfg.MaxInFlight <= 0 || cfg.MaxBodyBytes <= 0 {
		panic("invalid HTTP limits")
	}
	now := deps.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	a := &AdminApp{
		cfg: cfg, store: deps.Store, limiter: deps.Limiter,
		bodyMax: cfg.MaxBodyBytes, admit: make(chan struct{}, cfg.MaxInFlight), now: now,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("GET /admin/", a.adminConsole)
	mux.HandleFunc("GET /admin/admin.css", a.adminAsset("text/css; charset=utf-8", adminCSS))
	mux.HandleFunc("GET /admin/admin.js", a.adminAsset("text/javascript; charset=utf-8", adminJS))
	mux.HandleFunc("GET /favicon.svg", a.adminAsset("image/svg+xml", []byte(adminFavicon)))
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /admin/v1/auth/sign-in", a.signIn)
	mux.HandleFunc("POST /admin/v1/auth/sign-out", a.withAdmin(true, a.signOut))
	mux.HandleFunc("POST /admin/v1/posts/{id}/decision", a.withAdmin(true, a.decidePost))
	mux.HandleFunc("POST /admin/v1/comments/{id}/decision", a.withAdmin(true, a.decideComment))
	mux.HandleFunc("GET /admin/v1/reports", a.withAdmin(false, a.adminReports))
	mux.HandleFunc("POST /admin/v1/reports/{id}/resolve", a.withAdmin(true, a.adminResolveReport))
	mux.HandleFunc("GET /admin/v1/appeals", a.withAdmin(false, a.adminAppeals))
	mux.HandleFunc("POST /admin/v1/appeals/{id}/resolve", a.withAdmin(true, a.adminResolveAppeal))
	mux.HandleFunc("POST /admin/v1/agents/{id}/suspend", a.withAdmin(true, a.adminSuspendAgent))
	return a.guard(mux)
}

func (a *AdminApp) guard(next http.Handler) http.Handler {
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

type adminLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	TOTPCode string `json:"totp_code"`
}

type moderationRequest struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

func (a *AdminApp) signIn(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		fail(w, r, http.StatusServiceUnavailable, "dependencies_unavailable")
		return
	}
	var req adminLoginRequest
	if _, ok := a.readJSON(w, r, &req); !ok {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if len(req.Username) < 3 || len(req.Username) > 80 || len(req.Password) < 12 {
		fail(w, r, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !a.rateLimit(w, r, "admin:auth:ip:"+clientIPFrom(r, a.cfg.TrustedProxyCIDRs)) {
		return
	}
	admin, err := a.store.GetAdminByUsername(r.Context(), req.Username)
	if err != nil || admin.DisabledAt != nil {
		fail(w, r, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !auth.VerifyPassword(admin.PasswordHash, req.Password) ||
		!auth.VerifyTOTP(admin.TOTPSecret, req.TOTPCode, a.now()) {
		fail(w, r, http.StatusUnauthorized, "unauthorized")
		return
	}
	token, session, err := a.store.CreateAdminSession(r.Context(), admin.ID, a.cfg.AdminSessionTTL)
	if err != nil {
		failStore(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: adminCookieName(a.cfg.CookieSecure), Value: token, Path: "/", HttpOnly: true,
		Secure: a.cfg.CookieSecure, SameSite: http.SameSiteStrictMode,
		MaxAge: int(a.cfg.AdminSessionTTL.Seconds()), Expires: session.ExpiresAt,
	})
	csrf, err := auth.CSRFToken(session.ID, session.CSRFKey)
	if err != nil {
		fail(w, r, http.StatusServiceUnavailable, "dependencies_unavailable")
		return
	}
	respondData(w, r, http.StatusOK, store.AdminSessionResult{CSRFToken: csrf, ExpiresAt: session.ExpiresAt})
}

func (a *AdminApp) currentAdminSession(r *http.Request) (store.AdminSession, bool) {
	if a.store == nil {
		return store.AdminSession{}, false
	}
	cookie, err := r.Cookie(adminCookieName(a.cfg.CookieSecure))
	if err != nil || cookie.Value == "" {
		return store.AdminSession{}, false
	}
	session, err := a.store.GetAdminSession(r.Context(), cookie.Value)
	if err != nil {
		return store.AdminSession{}, false
	}
	return session, true
}

func (a *AdminApp) withAdmin(requireCSRF bool, action func(http.ResponseWriter, *http.Request, store.AdminSession)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.store == nil {
			fail(w, r, http.StatusServiceUnavailable, "dependencies_unavailable")
			return
		}
		session, ok := a.currentAdminSession(r)
		if !ok {
			fail(w, r, http.StatusUnauthorized, "unauthorized")
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), actorKey{}, "admin:"+session.AdminID.String()))
		if requireCSRF && !auth.VerifyCSRF(session.ID, session.CSRFKey, r.Header.Get("X-CSRF-Token")) {
			fail(w, r, http.StatusForbidden, "insufficient_scope")
			return
		}
		action(w, r, session)
	}
}

func (a *AdminApp) signOut(w http.ResponseWriter, r *http.Request, session store.AdminSession) {
	if err := a.store.RevokeAdminSession(r.Context(), session.ID); err != nil {
		failStore(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: adminCookieName(a.cfg.CookieSecure), Value: "", Path: "/", HttpOnly: true,
		Secure: a.cfg.CookieSecure, SameSite: http.SameSiteStrictMode,
		MaxAge: -1, Expires: time.Unix(0, 0).UTC(),
	})
	respondJSON(w, r, http.StatusOK, map[string]string{"status": "signed_out"})
}

func (a *AdminApp) decidePost(w http.ResponseWriter, r *http.Request, session store.AdminSession) {
	postID, ok := parseUUIDPath(w, r, "id")
	if !ok {
		return
	}
	var req moderationRequest
	body, ok := a.readJSON(w, r, &req)
	if !ok {
		return
	}
	key, ok := a.idempotencyKey(w, r)
	if !ok {
		return
	}
	if (req.Decision != "approve" && req.Decision != "reject") || !validReason(req.Reason, store.MaxReasonRunes) {
		fail(w, r, http.StatusUnprocessableEntity, "invalid_input")
		return
	}
	post, err := a.store.AdminDecidePost(r.Context(), session.AdminID, postID, req.Decision,
		req.Reason, requestHash(body), "admin.post.decision", key)
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusOK, post)
}

func (a *AdminApp) decideComment(w http.ResponseWriter, r *http.Request, session store.AdminSession) {
	commentID, ok := parseUUIDPath(w, r, "id")
	if !ok {
		return
	}
	var req moderationRequest
	body, ok := a.readJSON(w, r, &req)
	if !ok {
		return
	}
	key, ok := a.idempotencyKey(w, r)
	if !ok {
		return
	}
	if (req.Decision != "approve" && req.Decision != "reject") || !validReason(req.Reason, store.MaxReasonRunes) {
		fail(w, r, http.StatusUnprocessableEntity, "invalid_input")
		return
	}
	comment, err := a.store.AdminDecideComment(r.Context(), session.AdminID, commentID, req.Decision,
		req.Reason, requestHash(body), "admin.comment.decision", key)
	if err != nil {
		failStore(w, r, err)
		return
	}
	respondData(w, r, http.StatusOK, comment)
}

func (a *AdminApp) readJSON(w http.ResponseWriter, r *http.Request, dst any) ([]byte, bool) {
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

func (a *AdminApp) idempotencyKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := r.Header.Get("Idempotency-Key")
	if !idempotencyPattern.MatchString(key) {
		fail(w, r, http.StatusBadRequest, "idempotency_key_required")
		return "", false
	}
	return key, true
}

func (a *AdminApp) rateLimit(w http.ResponseWriter, r *http.Request, key string) bool {
	if a.limiter == nil {
		fail(w, r, http.StatusServiceUnavailable, "limiter_unavailable")
		return false
	}
	ok, err := a.limiter.Allow(r.Context(), key, int64(a.cfg.RedisAuthPerMin), float64(a.cfg.RedisAuthPerMin)/60.0, 1)
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
