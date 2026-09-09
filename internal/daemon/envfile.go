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

// Setting is one line of the configuration file.
type Setting struct {
	Key, Value string
	// Comment goes above the line, without the leading hash.
	Comment string
}

// WriteConfigFile writes settings to path, replacing whatever is there.
//
// The file holds tokens, so it is created with owner-only permissions and
// written atomically. It is also read by systemd when the daemon runs as a
// unit, which is why the format is bare KEY=value with no quoting and no
// export: a file that behaved differently depending on who read it would be
// worse than either format alone.
func WriteConfigFile(path string, settings []Setting) error {
	var b strings.Builder
	b.WriteString("# Written by `nanoclaude setup`.\n")
	b.WriteString("# Format is systemd EnvironmentFile: bare KEY=value, no export, no quotes.\n")
	for _, s := range settings {
		b.WriteString("\n")
		if s.Comment != "" {
			b.WriteString("# " + s.Comment + "\n")
		}
		b.WriteString(s.Key + "=" + s.Value + "\n")
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".env-*")
	if err != nil {
		return fmt.Errorf("daemon: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("daemon: chmod temp file: %w", err)
	}
	if _, err := tmp.WriteString(b.String()); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("daemon: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("daemon: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("daemon: install config: %w", err)
	}
	return nil
}

// SetConfigValue writes one setting, leaving the rest of the file alone.
//
// Line based rather than parse-and-rewrite, for the same reason the hooks
// installer edits JSON in place: the file is meant to be read and edited by
// hand, and rewriting it to change one value would lose the comments and the
// order a person put there.
func SetConfigValue(path, key, value string) error {
	existing, err := os.ReadFile(path) //nolint:gosec // the user's own config file
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("daemon: read config: %w", err)
	}

	lines := strings.Split(strings.TrimRight(string(existing), "\n"), "\n")
	replaced := false
	for i, line := range lines {
		k, _, ok := parseEnvLine(line)
		if ok && k == key {
			lines[i] = key + "=" + value
			replaced = true
			break
		}
	}
	if !replaced {
		lines = append(lines, "", key+"="+value)
	}

	body := strings.Join(lines, "\n") + "\n"
	return writeFileAtomic(path, []byte(body))
}

// writeFileAtomic replaces a file's contents in one step, with owner-only
// permissions. The file holds tokens, and a crash halfway through a plain
// write would leave the daemon with half a configuration.
func writeFileAtomic(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("daemon: create config directory: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".env-*")
	if err != nil {
		return fmt.Errorf("daemon: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("daemon: chmod temp file: %w", err)
	}
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("daemon: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("daemon: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("daemon: install config: %w", err)
	}
	return nil
}
