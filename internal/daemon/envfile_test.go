package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseEnvLine(t *testing.T) {
	tests := []struct {
		raw        string
		key, value string
		ok         bool
	}{
		{"FOO=bar", "FOO", "bar", true},
		{"  FOO = bar  ", "FOO", "bar", true},
		{`FOO="bar baz"`, "FOO", "bar baz", true},
		{"FOO='bar'", "FOO", "bar", true},
		{"FOO=", "FOO", "", true},
		{"FOO=a=b", "FOO", "a=b", true},
		{"# comment", "", "", false},
		{"   # indented comment", "", "", false},
		{"", "", "", false},
		{"   ", "", "", false},
		{"no equals sign", "", "", false},
		{"=novalue", "", "", false},
		// systemd's format, not shell: `export` is not stripped, so a
		// line written for a shell is left alone rather than silently
		// producing a variable called "export FOO".
		{"export FOO=bar", "export FOO", "bar", true},
		// Nothing inside a value is interpreted.
		{"FOO=$HOME", "FOO", "$HOME", true},
		{"FOO=a b c", "FOO", "a b c", true},
	}

	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			key, value, ok := parseEnvLine(tc.raw)
			if ok != tc.ok || key != tc.key || value != tc.value {
				t.Errorf("parseEnvLine(%q) = %q, %q, %v; want %q, %q, %v",
					tc.raw, key, value, ok, tc.key, tc.value, tc.ok)
			}
		})
	}
}

func TestLoadConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "env")
	body := "# a comment\nNANOCLAUDE_TEST_A=from-file\nNANOCLAUDE_TEST_B=also-from-file\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// An existing value must win, so a launcher or the command line can
	// still override the file.
	t.Setenv("NANOCLAUDE_TEST_A", "from-environment")
	t.Setenv("NANOCLAUDE_TEST_B", "")
	if err := os.Unsetenv("NANOCLAUDE_TEST_B"); err != nil {
		t.Fatalf("unset: %v", err)
	}

	if err := LoadConfigFile(path); err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	if got := os.Getenv("NANOCLAUDE_TEST_A"); got != "from-environment" {
		t.Errorf("NANOCLAUDE_TEST_A = %q, want the environment to win", got)
	}
	if got := os.Getenv("NANOCLAUDE_TEST_B"); got != "also-from-file" {
		t.Errorf("NANOCLAUDE_TEST_B = %q, want it read from the file", got)
	}
}

// TestLoadConfigFileMissing covers a machine with no config file, which is
// not an error: the settings that matter report themselves missing later,
// with a better message.
func TestLoadConfigFileMissing(t *testing.T) {
	if err := LoadConfigFile(filepath.Join(t.TempDir(), "nope")); err != nil {
		t.Errorf("LoadConfigFile on a missing file: %v", err)
	}
}

func TestConfigFilePathHonoursXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")
	got, err := ConfigFilePath()
	if err != nil {
		t.Fatalf("ConfigFilePath: %v", err)
	}
	if want := "/custom/config/nanoclaude/env"; got != want {
		t.Errorf("ConfigFilePath() = %q, want %q", got, want)
	}
}
