package hermes

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"cortex.local/cortex/internal/config"
)

type SyncOptions struct {
	HermesHome string
	DataDir    string
	ServerURL  string
	RootAgent  string
	Activate   bool
}

type SyncedProfile struct {
	AgentID string `json:"agent_id"`
	Home    string `json:"home"`
}

type SyncResult struct {
	Profiles  []SyncedProfile `json:"profiles"`
	BackupDir string          `json:"backup_dir"`
}

type profilePlan struct {
	profile         SyncedProfile
	activatedConfig []byte
}

func Sync(options SyncOptions) (SyncResult, error) {
	if strings.TrimSpace(options.HermesHome) == "" || strings.TrimSpace(options.DataDir) == "" {
		return SyncResult{}, fmt.Errorf("Hermes home and Cortex data directory are required")
	}
	if options.RootAgent == "" {
		options.RootAgent = "mika"
	}
	if err := validateServerURL(options.ServerURL); err != nil {
		return SyncResult{}, err
	}
	if _, err := config.Load(options.DataDir); err != nil {
		return SyncResult{}, err
	}
	profiles, err := discoverProfiles(options.HermesHome, options.RootAgent)
	if err != nil {
		return SyncResult{}, err
	}
	plans := make([]profilePlan, 0, len(profiles))
	presentedTokens := make(map[string]string, len(profiles))
	for _, profile := range profiles {
		plan := profilePlan{profile: profile}
		if token, ok := presentedToken(profile.Home, profile.AgentID); ok {
			presentedTokens[profile.AgentID] = token
		}
		if options.Activate {
			plan.activatedConfig, err = renderActivatedConfig(profile.Home)
			if err != nil {
				return SyncResult{}, fmt.Errorf("prepare activation for %s: %w", profile.AgentID, err)
			}
		}
		plans = append(plans, plan)
	}
	snapshot, err := createSnapshot(options.DataDir, options.HermesHome, profiles)
	if err != nil {
		return SyncResult{}, fmt.Errorf("create Hermes rollback snapshot: %w", err)
	}
	result := SyncResult{BackupDir: snapshot.Dir}
	agentIDs := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		agentIDs = append(agentIDs, profile.AgentID)
	}
	tokens, err := config.ProvisionAgents(options.DataDir, agentIDs, presentedTokens)
	if err != nil {
		return rollbackSync(result, snapshot, fmt.Errorf("provision Cortex agents: %w", err))
	}
	for _, plan := range plans {
		profile := plan.profile
		if err := installProvider(profile.Home); err != nil {
			return rollbackSync(result, snapshot, fmt.Errorf("install connector for %s: %w", profile.AgentID, err))
		}
		if err := writeConnectorConfig(profile.Home, connectorConfig{
			URL: options.ServerURL, Token: tokens[profile.AgentID], AgentID: profile.AgentID,
		}); err != nil {
			return rollbackSync(result, snapshot, fmt.Errorf("configure connector for %s: %w", profile.AgentID, err))
		}
		if options.Activate {
			if err := writeAtomic(filepath.Join(profile.Home, "config.yaml"), plan.activatedConfig, 0o600); err != nil {
				return rollbackSync(result, snapshot, fmt.Errorf("activate connector for %s: %w", profile.AgentID, err))
			}
		}
	}
	result.Profiles = profiles
	return result, nil
}

func rollbackSync(result SyncResult, snapshot *rollbackSnapshot, cause error) (SyncResult, error) {
	if err := snapshot.Restore(); err != nil {
		return result, fmt.Errorf("%w; rollback failed: %v", cause, err)
	}
	return result, cause
}

func validateServerURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("Cortex server URL must be an absolute http(s) URL")
	}
	return nil
}

func discoverProfiles(hermesHome, rootAgent string) ([]SyncedProfile, error) {
	profiles := []SyncedProfile{{AgentID: strings.ToLower(rootAgent), Home: hermesHome}}
	entries, err := os.ReadDir(filepath.Join(hermesHome, "profiles"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("list Hermes profiles: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		profiles = append(profiles, SyncedProfile{
			AgentID: strings.ToLower(entry.Name()),
			Home:    filepath.Join(hermesHome, "profiles", entry.Name()),
		})
	}
	sort.Slice(profiles[1:], func(i, j int) bool {
		return profiles[i+1].AgentID < profiles[j+1].AgentID
	})
	return profiles, nil
}

func presentedToken(home, agentID string) (string, bool) {
	raw, err := os.ReadFile(filepath.Join(home, "cortex.json"))
	if err != nil {
		return "", false
	}
	var existing connectorConfig
	if json.Unmarshal(raw, &existing) != nil || existing.AgentID != agentID {
		return "", false
	}
	return existing.Token, existing.Token != ""
}
