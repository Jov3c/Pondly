package httpapi

import (
	"crypto/sha256"
	"encoding/json"
	"net/http"

	"github.com/Jov3c/Pondly/apps/api/internal/auth"
	"github.com/Jov3c/Pondly/apps/api/internal/store"
	"github.com/google/uuid"
)

func (a *App) ownerAuthStart(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		fail(w, r, http.StatusServiceUnavailable, "dependencies_unavailable")
		return
	}
	var req ownerAuthStartRequest
	if _, ok := a.readJSON(w, r, &req); !ok {
		return
	}
	email, err := auth.NormalizeEmail(req.Email)
	if err != nil {
		fail(w, r, http.StatusUnprocessableEntity, "invalid_input")
		return
	}
	purpose, ok := normalizePurpose(req.Purpose)
	if !ok {
		fail(w, r, http.StatusUnprocessableEntity, "invalid_input")
		return
	}
	if !a.rateLimitAuth(w, r, email) {
		return
	}
	if !a.rateLimitOTPStart(w, r, email) {
		return
	}
	var session *store.OwnerSession
	if purpose == "step_up" {
		current, err := a.ownerSession(r)
		if err != nil {
			fail(w, r, http.StatusUnauthorized, "unauthorized")
			return
		}
		ownerEmail, err := a.store.OwnerEmail(r.Context(), current.OwnerID)
		if err != nil {
			failStore(w, r, err)
			return
		}
		if ownerEmail != email {
			fail(w, r, http.StatusForbidden, "not_owner")
			return
		}
		session = &current
	}
	code, err := auth.NewNumericCode(6)
	if err != nil {
		fail(w, r, http.StatusServiceUnavailable, "dependencies_unavailable")
		return
	}
	codeHash := sha256.Sum256([]byte(code))
	var sessionID *uuid.UUID
	if session != nil {
		sessionID = &session.ID
	}
	if err := a.store.CreateOwnerOTP(r.Context(), email, codeHash, purpose, sessionID, a.cfg.OTPTTL); err != nil {
		failStore(w, r, err)
		return
	}
	if a.mailer == nil {
		fail(w, r, http.StatusServiceUnavailable, "dependencies_unavailable")
		return
	}
	if err := a.mailer.SendOTP(r.Context(), email, code); err != nil {
		fail(w, r, http.StatusServiceUnavailable, "dependencies_unavailable")
		return
	}
	respondData(w, r, http.StatusOK, map[string]string{"status": "if_account_exists_message_sent"})
}

func (a *App) ownerAuthVerify(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		fail(w, r, http.StatusServiceUnavailable, "dependencies_unavailable")
		return
	}
	var req ownerAuthVerifyRequest
	if _, ok := a.readJSON(w, r, &req); !ok {
		return
	}
	email, err := auth.NormalizeEmail(req.Email)
	if err != nil {
		fail(w, r, http.StatusUnprocessableEntity, "invalid_input")
		return
	}
	purpose, ok := normalizePurpose(req.Purpose)
	if !ok {
		fail(w, r, http.StatusUnprocessableEntity, "invalid_input")
		return
	}
	if !a.rateLimitAuth(w, r, email) {
		return
	}
	var current *store.OwnerSession
	if purpose == "step_up" {
		session, err := a.ownerSession(r)
		if err != nil {
			fail(w, r, http.StatusUnauthorized, "unauthorized")
			return
		}
		current = &session
	}
	codeHash := sha256.Sum256([]byte(req.Code))
	var sessionID *uuid.UUID
	if current != nil {
		sessionID = &current.ID
	}
	ownerID, err := a.store.VerifyOwnerOTP(r.Context(), email, codeHash, purpose, sessionID, a.now())
	if err != nil {
		fail(w, r, http.StatusUnauthorized, "unauthorized")
		return
	}
	if purpose == "step_up" {
		updated, err := a.store.StepUpOwnerSession(r.Context(), current.ID, a.now())
		if err != nil {
			fail(w, r, http.StatusUnauthorized, "unauthorized")
			return
		}
		a.respondOwnerSession(w, r, updated, false)
		return
	}
	token, session, err := a.store.CreateOwnerSession(r.Context(), ownerID, a.cfg.OwnerSessionTTL)
	if err != nil {
		failStore(w, r, err)
		return
	}
	setOwnerCookie(w, token, a.cfg.OwnerSessionTTL, a.cfg.CookieSecure)
	a.respondOwnerSession(w, r, session, true)
}

func (a *App) respondOwnerSession(w http.ResponseWriter, r *http.Request, session store.OwnerSession, _ bool) {
	csrf, err := auth.CSRFToken(session.ID, session.CSRFKey)
	if err != nil {
		fail(w, r, http.StatusServiceUnavailable, "dependencies_unavailable")
		return
	}
	stepUp := a.now()
	if session.StepUpAt != nil {
		stepUp = *session.StepUpAt
	}
	respondData(w, r, http.StatusOK, store.OwnerSessionResult{
		CSRFToken: csrf,
		ExpiresAt: session.ExpiresAt,
		StepUpAt:  stepUp,
	})
}

func (a *App) ownerSignOut(w http.ResponseWriter, r *http.Request, session store.OwnerSession) {
	if err := a.store.RevokeOwnerSession(r.Context(), session.ID); err != nil {
		failStore(w, r, err)
		return
	}
	clearOwnerCookie(w, a.cfg.CookieSecure)
	body, _ := json.Marshal(map[string]string{"status": "signed_out"})
	respondRaw(w, r, http.StatusOK, body)
}
