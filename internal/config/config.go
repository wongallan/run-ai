package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	configDirName  = ".rai"
	configFileName = "config"
)

// ConfigPath returns the path to the local config file for the given base directory.
func ConfigPath(baseDir string) string {
	return filepath.Join(baseDir, configDirName, configFileName)
}

// Load reads the local config file for the given base directory.
// Missing files are treated as empty configuration.
func Load(baseDir string) (map[string]string, error) {
	path := ConfigPath(baseDir)
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	defer file.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid config line %d", lineNumber)
		}
		key := strings.TrimSpace(parts[0])
		rawValue := strings.TrimSpace(parts[1])
		if key == "" {
			return nil, fmt.Errorf("invalid config line %d: empty key", lineNumber)
		}
		value := rawValue
		if strings.HasPrefix(rawValue, "\"") || strings.HasPrefix(rawValue, "'") {
			unquoted, err := strconv.Unquote(rawValue)
			if err != nil {
				return nil, fmt.Errorf("invalid config line %d: %w", lineNumber, err)
			}
			value = unquoted
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return values, nil
}

// Set updates a single key in the local config file, creating it if needed.
func Set(baseDir, key, value string) error {
	if strings.TrimSpace(key) == "" {
		return errors.New("config key cannot be empty")
	}

	values, err := Load(baseDir)
	if err != nil {
		return err
	}
	values[key] = value

	return save(baseDir, values)
}

func save(baseDir string, values map[string]string) error {
	configDir := filepath.Join(baseDir, configDirName)
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return err
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var builder strings.Builder
	for _, key := range keys {
		builder.WriteString(key)
		builder.WriteString(" = ")
		builder.WriteString(strconv.Quote(values[key]))
		builder.WriteString("\n")
	}

	path := ConfigPath(baseDir)
	return os.WriteFile(path, []byte(builder.String()), 0o644)
}
