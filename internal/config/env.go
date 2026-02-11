package config

import (
	"os"
	"strings"
)

// EnvValues returns configuration values sourced from RAI_ environment variables.
func EnvValues() map[string]string {
	values := map[string]string{}
	for _, entry := range os.Environ() {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		name := parts[0]
		if !strings.HasPrefix(name, "RAI_") {
			continue
		}
		key := strings.TrimPrefix(name, "RAI_")
		if key == "" {
			continue
		}
		values[strings.ToLower(key)] = parts[1]
	}
	return values
}
