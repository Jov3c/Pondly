package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/Jov3c/Pondly/apps/api/internal/store"
)

type requestIDKey struct{}
type actorKey struct{}

func requestIDFrom(r *http.Request) string {
	if value, ok := r.Context().Value(requestIDKey{}).(string); ok {
		return value
	}
	return ""
}

func actorFrom(r *http.Request) string {
	if value, ok := r.Context().Value(actorKey{}).(string); ok {
		return value
	}
	return ""
}

func respondData(w http.ResponseWriter, r *http.Request, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

func respondRaw(w http.ResponseWriter, r *http.Request, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func respondJSON(w http.ResponseWriter, r *http.Request, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func fail(w http.ResponseWriter, r *http.Request, status int, code string) {
	if status == http.StatusServiceUnavailable || status == http.StatusTooManyRequests {
		w.Header().Set("Retry-After", "60")
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	log.Printf("httpapi error request_id=%s method=%s path=%s status=%d code=%s actor=%s",
		requestIDFrom(r), r.Method, r.URL.Path, status, code, actorFrom(r))
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{
			"code":       code,
			"message":    http.StatusText(status),
			"request_id": requestIDFrom(r),
		},
	})
}

func failStore(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(w, r, http.StatusNotFound, "not_found")
	case errors.Is(err, store.ErrUnauthorized):
		fail(w, r, http.StatusUnauthorized, "unauthorized")
	case errors.Is(err, store.ErrNotOwner):
		fail(w, r, http.StatusForbidden, "not_owner")
	case errors.Is(err, store.ErrNotMember):
		fail(w, r, http.StatusForbidden, "not_member")
	case errors.Is(err, store.ErrInsufficientScope):
		fail(w, r, http.StatusForbidden, "insufficient_scope")
	case errors.Is(err, store.ErrInactive):
		fail(w, r, http.StatusForbidden, "agent_inactive")
	case errors.Is(err, store.ErrStepUpRequired):
		fail(w, r, http.StatusForbidden, "step_up_required")
	case errors.Is(err, store.ErrEnrollmentInvalid):
		fail(w, r, http.StatusConflict, "enrollment_invalid")
	case errors.Is(err, store.ErrChallengeExpired), errors.Is(err, store.ErrExpired):
		fail(w, r, http.StatusConflict, "challenge_expired")
	case errors.Is(err, store.ErrChallengeUsed):
		fail(w, r, http.StatusConflict, "challenge_used")
	case errors.Is(err, store.ErrIdempotencyConflict):
		fail(w, r, http.StatusConflict, "idempotency_conflict")
	case errors.Is(err, store.ErrIdempotencyExpired):
		fail(w, r, http.StatusConflict, "idempotency_expired")
	case errors.Is(err, store.ErrInvalidCursor):
		fail(w, r, http.StatusBadRequest, "invalid_cursor")
	case errors.Is(err, store.ErrQuota), errors.Is(err, store.ErrCooldown):
		w.Header().Set("Retry-After", "60")
		fail(w, r, http.StatusTooManyRequests, "rate_limited")
	case errors.Is(err, store.ErrInvalid):
		fail(w, r, http.StatusUnprocessableEntity, "invalid_input")
	case errors.Is(err, store.ErrDisplayNameTaken):
		fail(w, r, http.StatusConflict, "display_name_taken")
	case errors.Is(err, store.ErrConflict):
		fail(w, r, http.StatusConflict, "conflict")
	case errors.Is(err, store.ErrReplayUnavailable):
		fail(w, r, http.StatusServiceUnavailable, "dependencies_unavailable")
	default:
		fail(w, r, http.StatusServiceUnavailable, "dependencies_unavailable")
	}
}
