package daemon

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// maxEnvFile caps the config read. It holds a handful of settings.
const maxEnvFile = 1 << 16

// ConfigFilePath returns the daemon's own configuration file, honouring
// XDG_CONFIG_HOME.
func ConfigFilePath() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" || !filepath.IsAbs(base) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("daemon: locate home: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "nanoclaude", "env"), nil
}

// LoadConfigFile reads settings from the config file into the process
// environment, leaving any variable that is already set alone.
//
// The daemon has to be able to configure itself from disk, because it is
// started by a Claude Code hook and therefore inherits Claude Code's
// environment -- which knows nothing about panels or tokens. Relying on the
// launcher to supply them worked only while systemd was doing it.
//
// Variables already present win, so an explicit setting on the command line
// or in a unit file still overrides the file.
//
// A missing file is not an error: everything in it has a default or is
// reported missing later, with a better message than this could give.
func LoadConfigFile(path string) error {
	f, err := os.Open(path) //nolint:gosec // path is the user's own config location
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("daemon: open config: %w", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(io.LimitReader(f, maxEnvFile))
	for line := 1; scanner.Scan(); line++ {
		key, value, ok := parseEnvLine(scanner.Text())
		if !ok {
			continue
		}
		if _, set := os.LookupEnv(key); set {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("daemon: set %s from config line %d: %w", key, line, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("daemon: read config: %w", err)
	}
	return nil
}

// parseEnvLine reads one KEY=VALUE line.
//
// This deliberately matches systemd's EnvironmentFile rules rather than
// shell: no `export`, no expansion, no command substitution. The same file
// has to work whether it is read here or by a unit, and a file that behaved
// differently depending on who read it would be worse than either.
func parseEnvLine(raw string) (key, value string, ok bool) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}

	key, value, found := strings.Cut(line, "=")
	if !found {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", false
	}

	value = strings.TrimSpace(value)
	// Surrounding quotes are stripped, since people write them by habit,
	// but nothing inside them is interpreted.
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') ||
			(value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
	}
	return key, value, true
}
