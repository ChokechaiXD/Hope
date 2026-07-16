package localauth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestEnsureCreatesStablePrivateLauncherKey(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	first, err := Ensure(dataDir)
	if err != nil {
		t.Fatalf("ensure launcher key: %v", err)
	}
	second, err := Ensure(dataDir)
	if err != nil {
		t.Fatalf("ensure existing launcher key: %v", err)
	}
	if first != second {
		t.Fatal("launcher key rotated during an idempotent ensure")
	}
	raw, err := base64.RawURLEncoding.DecodeString(first)
	if err != nil || len(raw) != 32 {
		t.Fatalf("launcher key is not 256-bit base64url: len=%d err=%v", len(raw), err)
	}
	info, err := os.Stat(filepath.Join(dataDir, KeyFileName))
	if err != nil {
		t.Fatalf("stat launcher key: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("launcher key permissions = %o, want owner-only", info.Mode().Perm())
	}
}

func TestBrokerRejectsInvalidAndReplayedProofsAndConsumesCodeOnce(t *testing.T) {
	t.Parallel()

	secret := testSecret(7)
	broker, err := NewBroker(secret, "mika")
	if err != nil {
		t.Fatalf("new broker: %v", err)
	}
	now := time.Unix(1_700_000_000, 0).UTC()
	proof, err := NewProof(secret, now)
	if err != nil {
		t.Fatalf("new proof: %v", err)
	}
	invalid := proof
	invalid.Signature = testSecret(9)
	if broker.Authorize(invalid, now) {
		t.Fatal("broker accepted an invalid signature")
	}
	if !broker.Authorize(proof, now) {
		t.Fatal("broker rejected a valid proof")
	}
	if broker.Authorize(proof, now) {
		t.Fatal("broker accepted a replayed proof")
	}
	stale, err := NewProof(secret, now.Add(-proofTTL-time.Second))
	if err != nil {
		t.Fatalf("new stale proof: %v", err)
	}
	if broker.Authorize(stale, now) {
		t.Fatal("broker accepted a stale proof")
	}
	code, err := broker.Issue(now)
	if err != nil {
		t.Fatalf("issue dashboard code: %v", err)
	}
	if agentID, ok := broker.Consume(code, now.Add(time.Second)); !ok || agentID != "mika" {
		t.Fatalf("consume dashboard code = %q, %v", agentID, ok)
	}
	if _, ok := broker.Consume(code, now.Add(2*time.Second)); ok {
		t.Fatal("dashboard code was reusable")
	}
	expired, err := broker.Issue(now)
	if err != nil {
		t.Fatalf("issue expiring dashboard code: %v", err)
	}
	if _, ok := broker.Consume(expired, now.Add(codeTTL+time.Second)); ok {
		t.Fatal("broker accepted an expired dashboard code")
	}
}

func TestRequestDashboardURLUsesSignedProofAndRejectsForeignURLs(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	secret, err := Ensure(dataDir)
	if err != nil {
		t.Fatalf("ensure launcher key: %v", err)
	}
	var received Proof
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		received = ProofFromHeader(request.Header)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(writer).Encode(map[string]string{"path": "/ui/session?code=once"})
	}))
	defer server.Close()

	url, err := RequestDashboardURL(context.Background(), server.Client(), dataDir, server.URL)
	if err != nil {
		t.Fatalf("request dashboard URL: %v", err)
	}
	if url != server.URL+"/ui/session?code=once" {
		t.Fatalf("dashboard URL = %q", url)
	}
	broker, err := NewBroker(secret, "mika")
	if err != nil {
		t.Fatalf("new broker: %v", err)
	}
	if !broker.Authorize(received, time.Now().UTC()) {
		t.Fatal("client did not send a valid launcher proof")
	}

	foreign := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"path":"https://example.com/steal"}`))
	}))
	defer foreign.Close()
	if _, err := RequestDashboardURL(context.Background(), foreign.Client(), dataDir, foreign.URL); err == nil {
		t.Fatal("client accepted a foreign dashboard URL")
	}
}

func testSecret(value byte) string {
	return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{value}, 32))
}
