package main

import "testing"

// TestBuildVersionPrefersTheStampedValue covers the version a released binary
// reports, which is the one set at build time.
func TestBuildVersionPrefersTheStampedValue(t *testing.T) {
	original := version
	t.Cleanup(func() { version = original })

	version = "v1.2.3"
	if got := buildVersion(); got != "v1.2.3" {
		t.Errorf("version reported as %q, want the value built in", got)
	}
}

// TestBuildVersionFallsBackToTheModule covers `go install`, which sets no
// build flags at all.
//
// A test binary records its own module version as "(devel)", so what this
// pins is the shape of the fallback rather than a released number: an
// unstamped build reports something, and never the empty string.
func TestBuildVersionFallsBackToTheModule(t *testing.T) {
	original := version
	t.Cleanup(func() { version = original })

	version = "dev"
	got := buildVersion()
	if got == "" {
		t.Fatal("an unstamped build reports no version at all")
	}
	if got == "(devel)" {
		t.Errorf("version reported as %q, which says less than %q", got, "dev")
	}
}
