package daemon

import (
	"os"
	"path/filepath"
	"strings"
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

// The shape of a real configuration file: comments the user wants kept, keys
// in a deliberate order, and one key commented out as documentation.
const realisticConfig = `# Written by ` + "`nanoclaude setup`" + `.
# Format is systemd EnvironmentFile: bare KEY=value.

# The panel controller on your network.
NANOCLAUDE_NANOLEAF_HOST=10.0.0.195

NANOCLAUDE_NANOLEAF_TOKEN=secret

# Pin the scale in dollars.
#NANOCLAUDE_CEILING_COST=250

# Extra rotation if the wall and the device disagree.
#NANOCLAUDE_ROTATION=0
`

func TestSetConfigValueReplacesInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "env")
	if err := os.WriteFile(path, []byte(realisticConfig), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := SetConfigValue(path, EnvNanoleafHost, "192.168.1.9"); err != nil {
		t.Fatalf("SetConfigValue: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := string(body)

	if !strings.Contains(got, EnvNanoleafHost+"=192.168.1.9") {
		t.Errorf("the new value is missing:\n%s", got)
	}
	if strings.Contains(got, "10.0.0.195") {
		t.Errorf("the old value is still there:\n%s", got)
	}
	if strings.Count(got, EnvNanoleafHost+"=") != 1 {
		t.Errorf("the key appears %d times, want 1", strings.Count(got, EnvNanoleafHost+"="))
	}
	// The comments and the other keys are the reason this is line based.
	for _, want := range []string{
		"# The panel controller on your network.",
		"NANOCLAUDE_NANOLEAF_TOKEN=secret",
		"#NANOCLAUDE_CEILING_COST=250",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("%q was lost:\n%s", want, got)
		}
	}
}

// TestSetConfigValueLeavesCommentsCommented is the case a user's file
// actually presents: the key is present but commented out as documentation.
// Uncommenting it would be a guess about intent, so a real line is appended
// instead, and that is the one the loader reads.
func TestSetConfigValueLeavesCommentsCommented(t *testing.T) {
	path := filepath.Join(t.TempDir(), "env")
	if err := os.WriteFile(path, []byte(realisticConfig), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := SetConfigValue(path, EnvRotation, "58"); err != nil {
		t.Fatalf("SetConfigValue: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := string(body)

	if !strings.Contains(got, "#NANOCLAUDE_ROTATION=0") {
		t.Error("the documentation comment was rewritten")
	}
	if !strings.Contains(got, "\nNANOCLAUDE_ROTATION=58") {
		t.Errorf("the real value was not added:\n%s", got)
	}

	// And the loader must see the real line, not the comment.
	t.Setenv(EnvRotation, "")
	if err := os.Unsetenv(EnvRotation); err != nil {
		t.Fatalf("unset: %v", err)
	}
	if err := LoadConfigFile(path); err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	if v := os.Getenv(EnvRotation); v != "58" {
		t.Errorf("loaded %q, want 58", v)
	}
}

func TestSetConfigValueCreatesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "env")

	if err := SetConfigValue(path, EnvRotation, "12"); err != nil {
		t.Fatalf("SetConfigValue: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// The file holds tokens, so the permissions are part of the contract.
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("permissions are %o, want 600", perm)
	}
}

// TestSetConfigValueIsRepeatable checks a second write replaces the first
// rather than stacking, which is what happens when someone calibrates twice.
func TestSetConfigValueIsRepeatable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "env")

	for _, v := range []string{"122", "58", "0"} {
		if err := SetConfigValue(path, EnvRotation, v); err != nil {
			t.Fatalf("SetConfigValue(%s): %v", v, err)
		}
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if n := strings.Count(string(body), EnvRotation+"="); n != 1 {
		t.Errorf("the key appears %d times after three writes, want 1:\n%s", n, body)
	}
	if !strings.Contains(string(body), EnvRotation+"=0") {
		t.Errorf("the last value did not win:\n%s", body)
	}
}
