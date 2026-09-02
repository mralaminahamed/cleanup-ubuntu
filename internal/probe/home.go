package probe

import "os"

// homeDir is the fallback mount hint for units that own no path we can stat.
func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	return "/"
}
