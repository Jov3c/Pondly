package auth

import (
	"bytes"
	"crypto/rand"
	"encoding/base32"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAgentTokenShapeAndHash(t *testing.T) {
	plain, hash, err := NewAgentToken()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(plain, "pndl_") || len(plain) < 40 {
		t.Fatalf("unexpected token %q", plain)
	}
	if HashToken(plain) != hash {
		t.Fatal("hash mismatch")
	}
	other, otherHash, err := NewAgentToken()
	if err != nil {
		t.Fatal(err)
	}
	if other == plain || otherHash == hash {
		t.Fatal("tokens must be unique")
	}
	if err := ValidTokenShape(plain); err != nil {
		t.Fatal(err)
	}
	if err := ValidTokenShape("bad token with spaces"); err == nil {
		t.Fatal("expected invalid token")
	}
}

func TestPasswordHashAndVerify(t *testing.T) {
	encoded, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(encoded, "correct horse battery staple") {
		t.Fatal("valid password rejected")
	}
	if VerifyPassword(encoded, "wrong password value") {
		t.Fatal("wrong password accepted")
	}
	if VerifyPassword("not-a-hash", "anything") {
		t.Fatal("malformed hash accepted")
	}
	if _, err := HashPassword("short"); err == nil {
		t.Fatal("short password accepted")
	}
}

func TestTOTP(t *testing.T) {
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	code, err := TOTPCode(secret, time.Unix(59, 0))
	if err != nil {
		t.Fatal(err)
	}
	if code != "287082" {
		t.Fatalf("code = %s, want 287082", code)
	}
	if !VerifyTOTP(secret, code, time.Unix(59, 0)) {
		t.Fatal("valid code rejected")
	}
	if VerifyTOTP(secret, "000000", time.Unix(59, 0)) {
		t.Fatal("wrong code accepted")
	}
	if !VerifyTOTP(secret, code, time.Unix(89, 0)) {
		t.Fatal("one-step window rejected")
	}
	if _, err := TOTPCode("not-base32!", time.Now()); err == nil {
		t.Fatal("invalid secret accepted")
	}
}

func TestCSRF(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	session := uuid.New()
	token, err := CSRFToken(session, key)
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyCSRF(session, key, token) {
		t.Fatal("valid csrf rejected")
	}
	if VerifyCSRF(uuid.New(), key, token) {
		t.Fatal("csrf accepted for another session")
	}
	if VerifyCSRF(session, key, token+"x") {
		t.Fatal("tampered csrf accepted")
	}
}

func TestCursor(t *testing.T) {
	key := bytes.Repeat([]byte{9}, 32)
	agent := uuid.New()
	token, err := EncodeCursor(key, agent, 42)
	if err != nil {
		t.Fatal(err)
	}
	gotAgent, gotSeq, err := DecodeCursor(key, token)
	if err != nil || gotAgent != agent || gotSeq != 42 {
		t.Fatalf("decode: %v %v %v", gotAgent, gotSeq, err)
	}
	if _, _, err := DecodeCursor(key, token+"x"); err == nil {
		t.Fatal("tampered cursor accepted")
	}
	if _, _, err := DecodeCursor(bytes.Repeat([]byte{8}, 32), token); err == nil {
		t.Fatal("wrong key accepted")
	}
	if _, _, err := DecodeCursor(key, "not-a-cursor"); err == nil {
		t.Fatal("malformed cursor accepted")
	}
	if _, err := EncodeCursor(key, agent, -1); err == nil {
		t.Fatal("negative seq accepted")
	}
}

func TestSecretbox(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	aad := []byte("principal|operation|key")
	plaintext := []byte(`{"token":"secret"}`)
	ciphertext, nonce, err := SealSecret(key, aad, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	got, err := OpenSecret(key, aad, ciphertext, nonce)
	if err != nil || !bytes.Equal(got, plaintext) {
		t.Fatalf("open: %q %v", got, err)
	}
	if _, err := OpenSecret(key, []byte("other"), ciphertext, nonce); err == nil {
		t.Fatal("wrong aad accepted")
	}
	otherKey := make([]byte, 32)
	if _, err := OpenSecret(otherKey, aad, ciphertext, nonce); err == nil {
		t.Fatal("wrong key accepted")
	}
	_, nonce2, err := SealSecret(key, aad, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(nonce, nonce2) {
		t.Fatal("nonce reused")
	}
}
