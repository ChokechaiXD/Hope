package localauth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	KeyFileName     = "launcher.key"
	HeaderTimestamp = "X-Cortex-Launcher-Time"
	HeaderNonce     = "X-Cortex-Launcher-Nonce"
	HeaderSignature = "X-Cortex-Launcher-Signature"
	proofTTL        = 30 * time.Second
	codeTTL         = 30 * time.Second
)

type Proof struct {
	Timestamp string
	Nonce     string
	Signature string
}

type Broker struct {
	key     []byte
	agentID string
	mu      sync.Mutex
	proofs  map[string]time.Time
	codes   map[string]time.Time
}

func Ensure(dataDir string) (string, error) {
	if strings.TrimSpace(dataDir) == "" {
		return "", fmt.Errorf("Cortex data directory is required")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return "", fmt.Errorf("create Cortex data directory: %w", err)
	}
	path := filepath.Join(dataDir, KeyFileName)
	if secret, err := Load(dataDir); err == nil {
		if err := os.Chmod(path, 0o600); err != nil {
			return "", fmt.Errorf("protect launcher key: %w", err)
		}
		return secret, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	secret, err := secureToken()
	if err != nil {
		return "", fmt.Errorf("generate launcher key: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return Load(dataDir)
	}
	if err != nil {
		return "", fmt.Errorf("create launcher key: %w", err)
	}
	if _, err := io.WriteString(file, secret+"\n"); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("write launcher key: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("sync launcher key: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close launcher key: %w", err)
	}
	return secret, nil
}

func Load(dataDir string) (string, error) {
	path := filepath.Join(dataDir, KeyFileName)
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", fmt.Errorf("launcher key must be a regular file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read launcher key: %w", err)
	}
	secret := strings.TrimSpace(string(raw))
	if _, err := decodeSecret(secret); err != nil {
		return "", err
	}
	return secret, nil
}

func NewProof(secret string, now time.Time) (Proof, error) {
	key, err := decodeSecret(secret)
	if err != nil {
		return Proof{}, err
	}
	nonce, err := secureToken()
	if err != nil {
		return Proof{}, fmt.Errorf("generate launcher nonce: %w", err)
	}
	proof := Proof{Timestamp: strconv.FormatInt(now.Unix(), 10), Nonce: nonce}
	proof.Signature = signature(key, proof.Timestamp, proof.Nonce)
	return proof, nil
}

func (proof Proof) Apply(header http.Header) {
	header.Set(HeaderTimestamp, proof.Timestamp)
	header.Set(HeaderNonce, proof.Nonce)
	header.Set(HeaderSignature, proof.Signature)
}

func ProofFromHeader(header http.Header) Proof {
	return Proof{
		Timestamp: strings.TrimSpace(header.Get(HeaderTimestamp)),
		Nonce:     strings.TrimSpace(header.Get(HeaderNonce)),
		Signature: strings.TrimSpace(header.Get(HeaderSignature)),
	}
}

func NewBroker(secret, agentID string) (*Broker, error) {
	key, err := decodeSecret(secret)
	if err != nil {
		return nil, err
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, fmt.Errorf("dashboard launcher agent is required")
	}
	return &Broker{
		key: append([]byte(nil), key...), agentID: agentID,
		proofs: make(map[string]time.Time), codes: make(map[string]time.Time),
	}, nil
}

func (broker *Broker) Authorize(proof Proof, now time.Time) bool {
	timestamp, err := strconv.ParseInt(proof.Timestamp, 10, 64)
	if err != nil || !validToken(proof.Nonce) {
		return false
	}
	issuedAt := time.Unix(timestamp, 0)
	age := now.Sub(issuedAt)
	if age < -proofTTL || age > proofTTL {
		return false
	}
	expected := signature(broker.key, proof.Timestamp, proof.Nonce)
	if len(expected) != len(proof.Signature) ||
		subtle.ConstantTimeCompare([]byte(expected), []byte(proof.Signature)) != 1 {
		return false
	}
	broker.mu.Lock()
	defer broker.mu.Unlock()
	broker.deleteExpiredLocked(now)
	if _, exists := broker.proofs[proof.Nonce]; exists {
		return false
	}
	broker.proofs[proof.Nonce] = now.Add(proofTTL)
	return true
}

func (broker *Broker) Issue(now time.Time) (string, error) {
	code, err := secureToken()
	if err != nil {
		return "", fmt.Errorf("generate dashboard code: %w", err)
	}
	broker.mu.Lock()
	defer broker.mu.Unlock()
	broker.deleteExpiredLocked(now)
	broker.codes[code] = now.Add(codeTTL)
	return code, nil
}

func (broker *Broker) Consume(code string, now time.Time) (string, bool) {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	broker.deleteExpiredLocked(now)
	expiresAt, exists := broker.codes[code]
	delete(broker.codes, code)
	if !exists || !expiresAt.After(now) {
		return "", false
	}
	return broker.agentID, true
}

func (broker *Broker) deleteExpiredLocked(now time.Time) {
	for nonce, expiresAt := range broker.proofs {
		if !expiresAt.After(now) {
			delete(broker.proofs, nonce)
		}
	}
	for code, expiresAt := range broker.codes {
		if !expiresAt.After(now) {
			delete(broker.codes, code)
		}
	}
}

func RequestDashboardURL(ctx context.Context, client *http.Client, dataDir, baseURL string) (string, error) {
	secret, err := Load(dataDir)
	if err != nil {
		return "", fmt.Errorf("load launcher key: %w", err)
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Scheme != "http" || base.Host == "" ||
		(base.Path != "" && base.Path != "/") || !loopbackHost(base.Hostname()) {
		return "", fmt.Errorf("dashboard base URL must be a loopback HTTP origin")
	}
	proof, err := NewProof(secret, time.Now().UTC())
	if err != nil {
		return "", err
	}
	endpoint := *base
	endpoint.Path = "/v1/dashboard/sessions"
	endpoint.RawQuery = ""
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), nil)
	if err != nil {
		return "", fmt.Errorf("create launcher request: %w", err)
	}
	proof.Apply(request.Header)
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	safeClient := *client
	safeClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := safeClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("request dashboard session: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("dashboard session endpoint returned %s", response.Status)
	}
	var payload struct {
		Path string `json:"path"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 2048))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return "", fmt.Errorf("decode dashboard session: %w", err)
	}
	relative, err := url.Parse(payload.Path)
	if err != nil || relative.IsAbs() || relative.Host != "" || relative.Path != "/ui/session" ||
		len(relative.Query()["code"]) != 1 || relative.Query().Get("code") == "" {
		return "", fmt.Errorf("dashboard session endpoint returned an invalid path")
	}
	return base.ResolveReference(relative).String(), nil
}

func signature(key []byte, timestamp, nonce string) string {
	mac := hmac.New(sha256.New, key)
	_, _ = io.WriteString(mac, timestamp)
	_, _ = io.WriteString(mac, "\n")
	_, _ = io.WriteString(mac, nonce)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func decodeSecret(secret string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(secret))
	if err != nil || len(raw) != 32 {
		return nil, fmt.Errorf("launcher key must be 256-bit base64url")
	}
	return raw, nil
}

func validToken(token string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(raw) == 32
}

func secureToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
