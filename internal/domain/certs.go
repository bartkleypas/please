package domain

import (
	"os"
	"path/filepath"
	"strings"
)

// ResolveCACert determines the effective CA certificate path with auto-discovery.
func ResolveCACert(caCertPath string, baseURL string) string {
	if caCertPath != "" {
		if strings.HasPrefix(caCertPath, "~/") || caCertPath == "~" {
			if home, err := os.UserHomeDir(); err == nil {
				caCertPath = filepath.Join(home, strings.TrimPrefix(caCertPath, "~"))
			}
		}
		return caCertPath
	}

	// Auto-discovery if connecting via HTTPS
	if strings.HasPrefix(baseURL, "https://") {
		if home, err := os.UserHomeDir(); err == nil {
			candidates := []string{
				filepath.Join(home, "Library", "Application Support", "please", "certs", "ca.crt"),
				filepath.Join(home, ".config", "please", "certs", "ca.crt"),
			}
			for _, c := range candidates {
				if _, err := os.Stat(c); err == nil {
					return c
				}
			}
		}
	}
	return ""
}
