package launcher

import "testing"

func TestValidateDashboardURLAllowsOnlyLoopbackRoot(t *testing.T) {
	t.Parallel()

	for _, valid := range []string{
		"http://127.0.0.1:7777/",
		"http://localhost:7777/",
		"http://[::1]:7777/",
	} {
		if err := validateDashboardURL(valid); err != nil {
			t.Errorf("validate %q: %v", valid, err)
		}
	}
	for _, invalid := range []string{
		"https://127.0.0.1:7777/",
		"http://example.com:7777/",
		"http://user@127.0.0.1:7777/",
		"http://127.0.0.1:7777/admin",
		"http://127.0.0.1:7777/?next=external",
		"http://127.0.0.1:7777/#fragment",
	} {
		if err := validateDashboardURL(invalid); err == nil {
			t.Errorf("validateDashboardURL(%q) succeeded", invalid)
		}
	}
}
