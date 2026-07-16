package config

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
)

const (
	FileName     = "config.json"
	DatabaseName = "cortex.db"
	fileVersion  = 1
)

var (
	agentIDPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	dashboardPINPattern = regexp.MustCompile(`^[0-9]{4,8}$`)
)

type Credential struct {
	AgentID   string `json:"agent_id"`
	TokenHash string `json:"token_sha256"`
}

type File struct {
	Version          int          `json:"version"`
	Listen           string       `json:"listen"`
	AdminAgents      []string     `json:"admin_agents"`
	Credentials      []Credential `json:"credentials"`
	DashboardPINHash string       `json:"dashboard_pin_sha256,omitempty"`
}

type ReloadingAuthenticator struct {
	dataDir string
	mu      sync.RWMutex
	current File
}

func NewReloadingAuthenticator(dataDir string) (*ReloadingAuthenticator, error) {
	current, err := Load(dataDir)
	if err != nil {
		return nil, err
	}
	return &ReloadingAuthenticator{dataDir: dataDir, current: current}, nil
}

func (auth *ReloadingAuthenticator) Authenticate(token string) (string, bool) {
	auth.reload()
	auth.mu.RLock()
	defer auth.mu.RUnlock()
	return auth.current.Authenticate(token)
}

func (auth *ReloadingAuthenticator) AuthenticateDashboard(secret string) (string, bool) {
	auth.reload()
	auth.mu.RLock()
	defer auth.mu.RUnlock()
	return auth.current.AuthenticateDashboard(secret)
}

func (auth *ReloadingAuthenticator) reload() {
	// ponytail: the config is intentionally re-read on auth; it is tiny and agent additions must work without restart.
	if latest, err := Load(auth.dataDir); err == nil {
		auth.mu.Lock()
		auth.current = latest
		auth.mu.Unlock()
	}
}

func Initialize(dataDir, adminAgent, listen string) (File, string, error) {
	adminAgent, err := normalizeAgentID(adminAgent)
	if err != nil {
		return File{}, "", err
	}
	listen = strings.TrimSpace(listen)
	if err := ValidateListen(listen); err != nil {
		return File{}, "", err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return File{}, "", fmt.Errorf("create data directory: %w", err)
	}
	token, hash, err := generateToken()
	if err != nil {
		return File{}, "", err
	}
	config := File{
		Version:     fileVersion,
		Listen:      listen,
		AdminAgents: []string{adminAgent},
		Credentials: []Credential{{AgentID: adminAgent, TokenHash: hash}},
	}
	if err := writeNew(filepath.Join(dataDir, FileName), config); err != nil {
		return File{}, "", err
	}
	return config, token, nil
}

func Load(dataDir string) (File, error) {
	path := filepath.Join(dataDir, FileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		return File{}, fmt.Errorf("read config: %w", err)
	}
	var config File
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return File{}, fmt.Errorf("decode config: %w", err)
	}
	if err := config.validate(); err != nil {
		return File{}, err
	}
	return config, nil
}

func AddAgent(dataDir, agentID string, admin bool) (string, error) {
	agentID, err := normalizeAgentID(agentID)
	if err != nil {
		return "", err
	}
	config, err := Load(dataDir)
	if err != nil {
		return "", err
	}
	if config.hasAgent(agentID) {
		return "", fmt.Errorf("agent %q already exists", agentID)
	}
	token, hash, err := generateToken()
	if err != nil {
		return "", err
	}
	config.Credentials = append(config.Credentials, Credential{AgentID: agentID, TokenHash: hash})
	if admin && !slices.Contains(config.AdminAgents, agentID) {
		config.AdminAgents = append(config.AdminAgents, agentID)
	}
	if err := writeAtomic(filepath.Join(dataDir, FileName), config); err != nil {
		return "", err
	}
	return token, nil
}

func IssueToken(dataDir, agentID string) (string, error) {
	agentID, err := normalizeAgentID(agentID)
	if err != nil {
		return "", err
	}
	config, err := Load(dataDir)
	if err != nil {
		return "", err
	}
	if !config.hasAgent(agentID) {
		return "", fmt.Errorf("agent %q does not exist", agentID)
	}
	token, hash, err := generateToken()
	if err != nil {
		return "", err
	}
	config.Credentials = append(config.Credentials, Credential{AgentID: agentID, TokenHash: hash})
	if err := writeAtomic(filepath.Join(dataDir, FileName), config); err != nil {
		return "", err
	}
	return token, nil
}

func SetDashboardPIN(dataDir, pin string) error {
	pin = strings.TrimSpace(pin)
	if !dashboardPINPattern.MatchString(pin) {
		return fmt.Errorf("dashboard PIN must contain 4 to 8 digits")
	}
	config, err := Load(dataDir)
	if err != nil {
		return err
	}
	config.DashboardPINHash = tokenHash(pin)
	return writeAtomic(filepath.Join(dataDir, FileName), config)
}

func (config File) Authenticate(token string) (string, bool) {
	hash := tokenHash(token)
	for _, credential := range config.Credentials {
		if subtle.ConstantTimeCompare([]byte(hash), []byte(credential.TokenHash)) == 1 {
			return credential.AgentID, true
		}
	}
	return "", false
}

func (config File) AuthenticateDashboard(secret string) (string, bool) {
	if config.DashboardPINHash == "" || len(config.AdminAgents) == 0 {
		return "", false
	}
	hash := tokenHash(strings.TrimSpace(secret))
	if subtle.ConstantTimeCompare([]byte(hash), []byte(config.DashboardPINHash)) != 1 {
		return "", false
	}
	return config.AdminAgents[0], true
}

func (config File) IsAdmin(agentID string) bool {
	return slices.Contains(config.AdminAgents, agentID)
}

func (config File) HasAgent(agentID string) bool {
	return config.hasAgent(strings.ToLower(strings.TrimSpace(agentID)))
}

func DatabasePath(dataDir string) string {
	return filepath.Join(dataDir, DatabaseName)
}

func DefaultDataDir() string {
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		return filepath.Join(localAppData, "Cortex")
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "Cortex"
	}
	return filepath.Join(configDir, "Cortex")
}

func (config File) validate() error {
	if config.Version != fileVersion {
		return fmt.Errorf("unsupported config version %d", config.Version)
	}
	if err := ValidateListen(config.Listen); err != nil {
		return fmt.Errorf("config %w", err)
	}
	knownAgents := make(map[string]struct{}, len(config.Credentials))
	seenHashes := make(map[string]struct{}, len(config.Credentials))
	for _, credential := range config.Credentials {
		if _, err := normalizeAgentID(credential.AgentID); err != nil {
			return err
		}
		if len(credential.TokenHash) != sha256.Size*2 {
			return fmt.Errorf("invalid token hash for agent %q", credential.AgentID)
		}
		if _, exists := seenHashes[credential.TokenHash]; exists {
			return fmt.Errorf("duplicate credential for agent %q", credential.AgentID)
		}
		knownAgents[credential.AgentID] = struct{}{}
		seenHashes[credential.TokenHash] = struct{}{}
	}
	for _, admin := range config.AdminAgents {
		if _, exists := knownAgents[admin]; !exists {
			return fmt.Errorf("admin agent %q has no credential", admin)
		}
	}
	if config.DashboardPINHash != "" && len(config.DashboardPINHash) != sha256.Size*2 {
		return fmt.Errorf("invalid dashboard PIN hash")
	}
	return nil
}

func (config File) hasAgent(agentID string) bool {
	for _, credential := range config.Credentials {
		if credential.AgentID == agentID {
			return true
		}
	}
	return false
}

func normalizeAgentID(agentID string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(agentID))
	if !agentIDPattern.MatchString(normalized) {
		return "", fmt.Errorf("agent id must match %s", agentIDPattern)
	}
	return normalized, nil
}

func generateToken() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	return token, tokenHash(token), nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func writeNew(path string, config File) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("config already exists at %s", path)
	}
	if err != nil {
		return fmt.Errorf("create config: %w", err)
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(config); err != nil {
		_ = file.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close config: %w", err)
	}
	return nil
}

func writeAtomic(path string, config File) error {
	directory := filepath.Dir(path)
	temp, err := os.CreateTemp(directory, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("protect temporary config: %w", err)
	}
	encoder := json.NewEncoder(temp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(config); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write temporary config: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary config: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}
