package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/Jov3c/Pondly/apps/api/internal/auth"
	"github.com/Jov3c/Pondly/apps/api/internal/testutil"
	"github.com/google/uuid"
)

func newIdentityStore(t *testing.T) (*Store, string) {
	t.Helper()
	ctx := context.Background()
	pool, url := testutil.StartPostgres(t)
	if err := Migrate(ctx, url, testutil.MigrationsDir(t)); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	s := &Store{
		Pool:      pool,
		ReplayKey: bytes.Repeat([]byte{4}, 32),
		ReplayTTL: 10 * time.Minute,
		Now:       func() time.Time { return now },
	}
	return s, url
}

func idemKey() string { return "idem-" + uuid.NewString()[:24] }

func requestHash(s string) [32]byte { return sha256.Sum256([]byte(s)) }

func TestRegisterAgentPropagatesStorageFailure(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	email, err := auth.NormalizeEmail("broken-store@example.com")
	if err != nil {
		t.Fatal(err)
	}
	codeHash := sha256.Sum256([]byte("123456"))
	if err := s.CreateOwnerOTP(ctx, email, codeHash, "sign_in", nil, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	ownerID, err := s.VerifyOwnerOTP(ctx, email, codeHash, "sign_in", nil, s.Now())
	if err != nil {
		t.Fatal(err)
	}
	enrollment, err := s.CreateEnrollmentTicket(ctx, ownerID, "owner.enrollment.create",
		idemKey(), requestHash("broken-enroll"), 15*time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}

	// The ticket lookup relation is gone: the read fails for a reason that has
	// nothing to do with the ticket, so the caller must not be told "invalid".
	if _, err := s.Pool.Exec(ctx, `DROP TABLE enrollment_tickets`); err != nil {
		t.Fatal(err)
	}
	_, err = s.RegisterAgent(ctx, "agent.register", idemKey(), requestHash("broken-register"),
		RegistrationInput{Handle: "brokentest", EnrollmentTicket: enrollment.EnrollmentTicket}, time.Hour)
	if err == nil {
		t.Fatal("storage failure was swallowed")
	}
	if errors.Is(err, ErrEnrollmentInvalid) {
		t.Fatalf("storage failure reported as an invalid ticket: %v", err)
	}
}

func TestOwnerAgentLifecycle(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	email, err := auth.NormalizeEmail("Owner@Example.COM")
	if err != nil {
		t.Fatal(err)
	}
	code := "123456"
	codeHash := sha256.Sum256([]byte(code))
	if err := s.CreateOwnerOTP(ctx, email, codeHash, "sign_in", nil, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	ownerID, err := s.VerifyOwnerOTP(ctx, email, codeHash, "sign_in", nil, s.Now())
	if err != nil {
		t.Fatal(err)
	}
	sessionToken, session, err := s.CreateOwnerSession(ctx, ownerID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetOwnerSession(ctx, sessionToken); err != nil {
		t.Fatal(err)
	}

	enrollKey := idemKey()
	enrollHash := requestHash("enroll")
	enrollment, err := s.CreateEnrollmentTicket(ctx, ownerID, "owner.enrollment.create", enrollKey, enrollHash, 15*time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.CreateEnrollmentTicket(ctx, ownerID, "owner.enrollment.create", enrollKey, enrollHash, 15*time.Minute, 1)
	if err != nil || replay.EnrollmentTicket != enrollment.EnrollmentTicket {
		t.Fatalf("enrollment replay: %+v %v", replay, err)
	}

	regKey := idemKey()
	regHash := requestHash("register")
	display := "测试鱼"
	framework := "dsh"
	avatar := "avatar-03"
	reg, err := s.RegisterAgent(ctx, "agent.register", regKey, regHash, RegistrationInput{
		Handle: "testfish", DisplayName: &display, FrameworkLabel: &framework, AvatarKey: &avatar,
		EnrollmentTicket: enrollment.EnrollmentTicket,
	}, 90*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if reg.Agent.Status != "active" || reg.Token == "" || reg.Agent.AvatarKey == nil || *reg.Agent.AvatarKey != avatar {
		t.Fatalf("registration: %+v", reg)
	}
	regReplay, err := s.RegisterAgent(ctx, "agent.register", regKey, regHash, RegistrationInput{
		Handle: "testfish", DisplayName: &display, FrameworkLabel: &framework, AvatarKey: &avatar,
		EnrollmentTicket: enrollment.EnrollmentTicket,
	}, 90*24*time.Hour)
	if err != nil || regReplay.Token != reg.Token || regReplay.Agent.ID != reg.Agent.ID {
		t.Fatalf("registration replay: %+v %v", regReplay, err)
	}
	if _, err := s.RegisterAgent(ctx, "agent.register", idemKey(), requestHash("different"),
		RegistrationInput{Handle: "secondfish", EnrollmentTicket: enrollment.EnrollmentTicket}, 90*24*time.Hour); !errors.Is(err, ErrEnrollmentInvalid) {
		t.Fatalf("consumed ticket must be rejected, got %v", err)
	}

	if _, err := s.CreateEnrollmentTicket(ctx, ownerID, "owner.enrollment.create", idemKey(), requestHash("second-enroll"), 15*time.Minute, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("owner must not issue a second agent ticket, got %v", err)
	}

	active, err := s.GetAgentByTokenHash(ctx, auth.HashToken(reg.Token))
	if err != nil || active.Agent.Status != "active" {
		t.Fatalf("token lookup: %+v %v", active, err)
	}

	rotated, err := s.RotateCredentials(ctx, reg.Agent.ID, "agent.credentials.rotate", idemKey(), requestHash("rotate"), 90*24*time.Hour)
	if err != nil || rotated.Token == "" {
		t.Fatal(rotated, err)
	}
	if _, err := s.GetAgentByTokenHash(ctx, auth.HashToken(reg.Token)); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("old token still valid: %v", err)
	}
	if _, err := s.GetAgentByTokenHash(ctx, auth.HashToken(rotated.Token)); err != nil {
		t.Fatalf("new token invalid: %v", err)
	}

	recovery, err := s.CreateRecoveryTicket(ctx, ownerID, reg.Agent.ID, session.StepUpAt, "owner.agent.recovery_ticket", idemKey(), requestHash("recovery"), 10*time.Minute, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := s.RecoverCredentials(ctx, reg.Agent.ID, recovery.RecoveryTicket, "agent.credentials.recover", idemKey(), requestHash("recover"), 90*24*time.Hour)
	if err != nil || recovered.Token == "" {
		t.Fatal(recovered, err)
	}
	if _, err := s.GetAgentByTokenHash(ctx, auth.HashToken(rotated.Token)); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("rotated token still valid after recovery: %v", err)
	}
	if _, err := s.GetAgentByTokenHash(ctx, auth.HashToken(recovered.Token)); err != nil {
		t.Fatalf("recovered token invalid: %v", err)
	}

	if suspended, err := s.SetAgentStatus(ctx, ownerID, reg.Agent.ID, "suspend", "owner.agent.suspend", idemKey(), requestHash("suspend")); err != nil || suspended.Status != "suspended" {
		t.Fatalf("suspend: %+v %v", suspended, err)
	}
	if resumed, err := s.SetAgentStatus(ctx, ownerID, reg.Agent.ID, "resume", "owner.agent.resume", idemKey(), requestHash("resume")); err != nil || resumed.Status != "active" {
		t.Fatalf("resume: %+v %v", resumed, err)
	}
	if _, err := s.SetAgentStatus(ctx, ownerID, reg.Agent.ID, "revoke", "owner.agent.revoke", idemKey(), requestHash("revoke")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAgentByTokenHash(ctx, auth.HashToken(recovered.Token)); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked token still valid: %v", err)
	}

	agents, err := s.ListOwnerAgents(ctx, ownerID)
	if err != nil || len(agents) != 1 || agents[0].ID != reg.Agent.ID {
		t.Fatalf("owner agents: %+v %v", agents, err)
	}
}

func TestSingleAgentAndTicketReplacement(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	ownerID := mustOwner(t, s, "single@example.com")

	first, err := s.CreateEnrollmentTicket(ctx, ownerID, "owner.enrollment.create",
		idemKey(), requestHash("first-enroll"), 15*time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateEnrollmentTicket(ctx, ownerID, "owner.enrollment.create",
		idemKey(), requestHash("second-enroll"), 15*time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterAgent(ctx, "agent.register", idemKey(), requestHash("old-register"),
		RegistrationInput{Handle: "oldfish", EnrollmentTicket: first.EnrollmentTicket}, time.Hour); !errors.Is(err, ErrEnrollmentInvalid) {
		t.Fatalf("replaced ticket must be invalid, got %v", err)
	}
	if _, err := s.RegisterAgent(ctx, "agent.register", idemKey(), requestHash("new-register"),
		RegistrationInput{Handle: "newfish", EnrollmentTicket: second.EnrollmentTicket}, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateEnrollmentTicket(ctx, ownerID, "owner.enrollment.create",
		idemKey(), requestHash("third-enroll"), 15*time.Minute, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("second agent ticket must be rejected, got %v", err)
	}
	agents, err := s.ListOwnerAgents(ctx, ownerID)
	if err != nil || len(agents) != 1 || agents[0].Handle != "newfish" {
		t.Fatalf("owner agents: %+v %v", agents, err)
	}
}

func TestEnrollmentProfileIsAppliedAtRegistration(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	ownerID := mustOwner(t, s, "profile-ticket@example.com")
	name := "小池"
	avatar := "avatar-10"
	enrollment, err := s.CreateEnrollmentTicketWithProfile(ctx, ownerID,
		EnrollmentProfile{DisplayName: &name, AvatarKey: &avatar},
		"owner.enrollment.create", idemKey(), requestHash("profile-ticket"), 15*time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := s.RegisterAgent(ctx, "agent.register", idemKey(), requestHash("profile-register"),
		RegistrationInput{Handle: "profilefish", EnrollmentTicket: enrollment.EnrollmentTicket}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if reg.Agent.DisplayName == nil || *reg.Agent.DisplayName != name ||
		reg.Agent.AvatarKey == nil || *reg.Agent.AvatarKey != avatar {
		t.Fatalf("ticket profile not applied: %+v", reg.Agent)
	}
}

func TestAgentDisplayNameUnique(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	name := "同名"

	firstOwner := mustOwner(t, s, "name-one@example.com")
	enrollment, err := s.CreateEnrollmentTicketWithProfile(ctx, firstOwner,
		EnrollmentProfile{DisplayName: &name},
		"owner.enrollment.create", idemKey(), requestHash("unique-name-ticket"), 15*time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterAgent(ctx, "agent.register", idemKey(), requestHash("unique-name-register"),
		RegistrationInput{Handle: "nameone", EnrollmentTicket: enrollment.EnrollmentTicket}, time.Hour); err != nil {
		t.Fatal(err)
	}

	secondOwner := mustOwner(t, s, "name-two@example.com")
	if _, err := s.CreateEnrollmentTicketWithProfile(ctx, secondOwner,
		EnrollmentProfile{DisplayName: &name},
		"owner.enrollment.create", idemKey(), requestHash("unique-name-ticket-two"), 15*time.Minute, 1); !errors.Is(err, ErrDisplayNameTaken) {
		t.Fatalf("duplicate enrollment name: %v", err)
	}

	caseOwner := mustOwner(t, s, "name-three@example.com")
	upper := "Koi"
	enrollment, err = s.CreateEnrollmentTicketWithProfile(ctx, caseOwner,
		EnrollmentProfile{DisplayName: &upper},
		"owner.enrollment.create", idemKey(), requestHash("unique-case-ticket"), 15*time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterAgent(ctx, "agent.register", idemKey(), requestHash("unique-case-register"),
		RegistrationInput{Handle: "koi", EnrollmentTicket: enrollment.EnrollmentTicket}, time.Hour); err != nil {
		t.Fatal(err)
	}
	lowerOwner := mustOwner(t, s, "name-four@example.com")
	lower := "koi"
	if _, err := s.CreateEnrollmentTicketWithProfile(ctx, lowerOwner,
		EnrollmentProfile{DisplayName: &lower},
		"owner.enrollment.create", idemKey(), requestHash("unique-case-ticket-two"), 15*time.Minute, 1); !errors.Is(err, ErrDisplayNameTaken) {
		t.Fatalf("case-insensitive duplicate name: %v", err)
	}

	raceOwner := mustOwner(t, s, "name-five@example.com")
	raceTicket, err := s.CreateEnrollmentTicket(ctx, raceOwner,
		"owner.enrollment.create", idemKey(), requestHash("unique-race-ticket"), 15*time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterAgent(ctx, "agent.register", idemKey(), requestHash("unique-race-register"),
		RegistrationInput{Handle: "racer", DisplayName: &name, EnrollmentTicket: raceTicket.EnrollmentTicket}, time.Hour); !errors.Is(err, ErrDisplayNameTaken) {
		t.Fatalf("duplicate name at registration: %v", err)
	}

	firstAgent, _, _ := createActiveAgent(t, s, "profile-a@example.com", "profilea")
	secondAgent, _, _ := createActiveAgent(t, s, "profile-b@example.com", "profileb")
	firstName := "Shared Name"
	if _, err := s.UpdateAgentProfile(ctx, firstAgent, ProfileInput{DisplayName: &firstName}); err != nil {
		t.Fatal(err)
	}
	duplicate := "shared name"
	if _, err := s.UpdateAgentProfile(ctx, secondAgent, ProfileInput{DisplayName: &duplicate}); !errors.Is(err, ErrDisplayNameTaken) {
		t.Fatalf("duplicate profile name: %v", err)
	}
}

func TestIdempotencyConflictAndExpiry(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	email := "owner@example.com"
	ownerID := mustOwner(t, s, email)

	key := idemKey()
	hash := requestHash("one")
	if _, err := s.CreateEnrollmentTicket(ctx, ownerID, "owner.enrollment.create", key, hash, 15*time.Minute, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateEnrollmentTicket(ctx, ownerID, "owner.enrollment.create", key, requestHash("two"), 15*time.Minute, 1); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	now := s.Now()
	s.Now = func() time.Time { return now.Add(11 * time.Minute) }
	if _, err := s.CreateEnrollmentTicket(ctx, ownerID, "owner.enrollment.create", key, hash, 15*time.Minute, 1); !errors.Is(err, ErrIdempotencyExpired) {
		t.Fatalf("expected expired replay, got %v", err)
	}
}

func mustOwner(t *testing.T, s *Store, email string) uuid.UUID {
	t.Helper()
	code := "654321"
	hash := sha256.Sum256([]byte(code))
	if err := s.CreateOwnerOTP(context.Background(), email, hash, "sign_in", nil, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	id, err := s.VerifyOwnerOTP(context.Background(), email, hash, "sign_in", nil, s.Now())
	if err != nil {
		t.Fatal(err)
	}
	return id
}
