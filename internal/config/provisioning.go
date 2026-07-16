package config

import "path/filepath"

// ProvisionAgents reuses presented valid tokens and writes every newly needed
// credential in one config replacement. This keeps multi-agent onboarding from
// exposing a partially provisioned credential set.
func ProvisionAgents(dataDir string, agentIDs []string, presented map[string]string) (map[string]string, error) {
	current, err := Load(dataDir)
	if err != nil {
		return nil, err
	}
	tokens := make(map[string]string, len(agentIDs))
	changed := false
	for _, rawAgentID := range agentIDs {
		agentID, err := normalizeAgentID(rawAgentID)
		if err != nil {
			return nil, err
		}
		if _, duplicate := tokens[agentID]; duplicate {
			continue
		}
		if candidate := presented[agentID]; candidate != "" {
			if authenticated, ok := current.Authenticate(candidate); ok && authenticated == agentID {
				tokens[agentID] = candidate
				continue
			}
		}
		token, hash, err := generateToken()
		if err != nil {
			return nil, err
		}
		current.Credentials = append(current.Credentials, Credential{AgentID: agentID, TokenHash: hash})
		tokens[agentID] = token
		changed = true
	}
	if changed {
		if err := writeAtomic(filepath.Join(dataDir, FileName), current); err != nil {
			return nil, err
		}
	}
	return tokens, nil
}
