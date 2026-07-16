package hermes

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	defaultAutoCaptureEveryTurns = 5
	defaultAutoCaptureMaxChars   = 1000
	defaultPrefetchTokenBudget   = 700
	defaultRecallTokenBudget     = 1200
)

var scopeKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

type ProfileSettings struct {
	AgentID               string `json:"agent_id"`
	DefaultProject        string `json:"default_project"`
	DefaultDomain         string `json:"default_domain"`
	AutoCaptureEnabled    bool   `json:"auto_capture_enabled"`
	AutoCaptureEveryTurns int    `json:"auto_capture_every_turns"`
	AutoCaptureMaxChars   int    `json:"auto_capture_max_chars"`
	PrefetchTokenBudget   int    `json:"prefetch_token_budget"`
	RecallTokenBudget     int    `json:"recall_token_budget"`
}

type UpdateProfileSettingsOptions struct {
	HermesHome string
	DataDir    string
	RootAgent  string
	Settings   ProfileSettings
}

type UpdateProfileSettingsResult struct {
	Settings   ProfileSettings
	BackupFile string
}

func ListProfileSettings(hermesHome, rootAgent string) ([]ProfileSettings, error) {
	profiles, err := discoverProfiles(hermesHome, rootAgent)
	if err != nil {
		return nil, err
	}
	settings := make([]ProfileSettings, 0, len(profiles))
	for _, profile := range profiles {
		value, document, err := readConnectorConfig(profile.Home)
		if err != nil {
			return nil, fmt.Errorf("read connector settings for %s: %w", profile.AgentID, err)
		}
		settings = append(settings, settingsFromConfig(profile.AgentID, value, document))
	}
	return settings, nil
}

func UpdateProfileSettings(options UpdateProfileSettingsOptions) (UpdateProfileSettingsResult, error) {
	settings := normalizedSettings(options.Settings)
	if err := validateProfileSettings(settings); err != nil {
		return UpdateProfileSettingsResult{}, err
	}
	profiles, err := discoverProfiles(options.HermesHome, options.RootAgent)
	if err != nil {
		return UpdateProfileSettingsResult{}, err
	}
	var profileHome string
	for _, profile := range profiles {
		if profile.AgentID == settings.AgentID {
			profileHome = profile.Home
			break
		}
	}
	if profileHome == "" {
		return UpdateProfileSettingsResult{}, fmt.Errorf("unknown Hermes agent %q", settings.AgentID)
	}
	value, _, err := readConnectorConfig(profileHome)
	if err != nil {
		return UpdateProfileSettingsResult{}, err
	}
	if value.AgentID != settings.AgentID || strings.TrimSpace(value.Token) == "" {
		return UpdateProfileSettingsResult{}, fmt.Errorf("Hermes agent %q is not connected to Cortex", settings.AgentID)
	}
	backupFile, err := backupConnectorSettings(options.DataDir, settings.AgentID, profileHome)
	if err != nil {
		return UpdateProfileSettingsResult{}, fmt.Errorf("backup connector settings: %w", err)
	}
	applyProfileSettings(&value, settings)
	if err := writeConnectorConfig(profileHome, value); err != nil {
		return UpdateProfileSettingsResult{}, fmt.Errorf("write connector settings: %w", err)
	}
	return UpdateProfileSettingsResult{Settings: settings, BackupFile: backupFile}, nil
}

func settingsFromConfig(agentID string, value connectorConfig, document map[string]json.RawMessage) ProfileSettings {
	settings := ProfileSettings{
		AgentID: agentID, DefaultProject: value.DefaultProject, DefaultDomain: value.DefaultDomain,
		AutoCaptureEnabled: value.AutoCaptureEnabled, AutoCaptureEveryTurns: value.AutoCaptureEveryTurns,
		AutoCaptureMaxChars: value.AutoCaptureMaxChars, PrefetchTokenBudget: value.PrefetchTokenBudget,
		RecallTokenBudget: value.RecallTokenBudget,
	}
	if _, exists := document["auto_capture_enabled"]; !exists {
		settings.AutoCaptureEnabled = true
	}
	if settings.AutoCaptureEveryTurns == 0 {
		settings.AutoCaptureEveryTurns = defaultAutoCaptureEveryTurns
	}
	if settings.AutoCaptureMaxChars == 0 {
		settings.AutoCaptureMaxChars = defaultAutoCaptureMaxChars
	}
	if settings.PrefetchTokenBudget == 0 {
		settings.PrefetchTokenBudget = defaultPrefetchTokenBudget
	}
	if settings.RecallTokenBudget == 0 {
		settings.RecallTokenBudget = defaultRecallTokenBudget
	}
	return settings
}

func applyProfileSettings(value *connectorConfig, settings ProfileSettings) {
	value.DefaultProject = settings.DefaultProject
	value.DefaultDomain = settings.DefaultDomain
	value.AutoCaptureEnabled = settings.AutoCaptureEnabled
	value.AutoCaptureEveryTurns = settings.AutoCaptureEveryTurns
	value.AutoCaptureMaxChars = settings.AutoCaptureMaxChars
	value.PrefetchTokenBudget = settings.PrefetchTokenBudget
	value.RecallTokenBudget = settings.RecallTokenBudget
}

func normalizedSettings(settings ProfileSettings) ProfileSettings {
	settings.AgentID = strings.ToLower(strings.TrimSpace(settings.AgentID))
	settings.DefaultProject = strings.ToLower(strings.TrimSpace(settings.DefaultProject))
	settings.DefaultDomain = strings.ToLower(strings.TrimSpace(settings.DefaultDomain))
	return settings
}

func validateProfileSettings(settings ProfileSettings) error {
	if !scopeKeyPattern.MatchString(settings.AgentID) {
		return fmt.Errorf("invalid Hermes agent id")
	}
	for label, value := range map[string]string{"project": settings.DefaultProject, "domain": settings.DefaultDomain} {
		if value != "" && !scopeKeyPattern.MatchString(value) {
			return fmt.Errorf("invalid %s key", label)
		}
	}
	if settings.AutoCaptureEveryTurns < 1 || settings.AutoCaptureEveryTurns > 50 ||
		settings.AutoCaptureMaxChars < 100 || settings.AutoCaptureMaxChars > 4000 ||
		settings.PrefetchTokenBudget < 100 || settings.PrefetchTokenBudget > 4000 ||
		settings.RecallTokenBudget < 100 || settings.RecallTokenBudget > 4000 {
		return fmt.Errorf("connector settings are outside supported limits")
	}
	return nil
}

func backupConnectorSettings(dataDir, agentID, profileHome string) (string, error) {
	backupDir := filepath.Join(dataDir, "backups")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return "", err
	}
	name := fmt.Sprintf("hermes-settings-%s-%s.json", agentID, time.Now().UTC().Format("20060102-150405.000000000"))
	destination := filepath.Join(backupDir, name)
	info, err := os.Stat(filepath.Join(profileHome, "cortex.json"))
	if err != nil {
		return "", err
	}
	if err := copyFile(filepath.Join(profileHome, "cortex.json"), destination, info.Mode().Perm()); err != nil {
		return "", err
	}
	return destination, nil
}
