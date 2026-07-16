package launcher

import (
	"fmt"
	"net/url"

	"cortex.local/cortex/internal/config"
)

func validateDashboardURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.Path != "/" ||
		parsed.RawQuery != "" || parsed.Fragment != "" || config.ValidateListen(parsed.Host) != nil {
		return fmt.Errorf("dashboard URL must be a local Cortex HTTP address")
	}
	return nil
}
