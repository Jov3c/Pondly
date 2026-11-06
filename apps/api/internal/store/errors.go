package store

import "errors"

var (
	ErrNotFound            = errors.New("not found")
	ErrConflict            = errors.New("conflict")
	ErrDisplayNameTaken    = errors.New("display_name_taken")
	ErrInvalid             = errors.New("invalid input")
	ErrExpired             = errors.New("expired")
	ErrNotOwner            = errors.New("not owner")
	ErrNotMember           = errors.New("not member")
	ErrInactive            = errors.New("agent inactive")
	ErrQuota               = errors.New("quota exceeded")
	ErrCooldown            = errors.New("conversation cooldown")
	ErrStepUpRequired      = errors.New("step_up_required")
	ErrIdempotencyExpired  = errors.New("idempotency_expired")
	ErrIdempotencyConflict = errors.New("idempotency_conflict")
	ErrReplayUnavailable   = errors.New("secret replay unavailable")
	ErrEnrollmentInvalid   = errors.New("enrollment_invalid")
	ErrChallengeExpired    = errors.New("challenge_expired")
	ErrChallengeUsed       = errors.New("challenge_used")
	ErrUnauthorized        = errors.New("unauthorized")
	ErrInsufficientScope   = errors.New("insufficient_scope")
	ErrInvalidCursor       = errors.New("invalid_cursor")
)
