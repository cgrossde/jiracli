package main

import (
	"bytes"
	"strings"
	"testing"
)

// runCapture invokes run() with the given args and returns combined stdout.
func runCapture(t *testing.T, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	_ = run(args, &stdout, &stderr)
	return stdout.String()
}

// When a required positional arg is entirely missing, the error output should
// include the command's full help (its Long description), not just the terse
// usage block.
func TestRun_MissingRequiredArg_ShowsDescription(t *testing.T) {
	out := runCapture(t, "hierarchy")

	// Long description marker.
	if !strings.Contains(out, "Map an issue's place in the work hierarchy") {
		t.Errorf("expected full help with description for bare `hierarchy`, got:\n%s", out)
	}
	// Usage + flags are still present.
	if !strings.Contains(out, "Usage:") || !strings.Contains(out, "--depth") {
		t.Errorf("expected usage + flags in output, got:\n%s", out)
	}
}

// A flag error (not a missing-arg error) should keep the terse usage block and
// must NOT include the long description.
func TestRun_FlagError_ShowsTerseUsageOnly(t *testing.T) {
	out := runCapture(t, "hierarchy", "--bogus")

	if strings.Contains(out, "Map an issue's place in the work hierarchy") {
		t.Errorf("flag error should not print the long description, got:\n%s", out)
	}
	if !strings.Contains(out, "Usage:") {
		t.Errorf("expected usage block for flag error, got:\n%s", out)
	}
}

// Too many args is an arg-count error but not a *missing* arg, so it should
// keep the terse usage block.
func TestRun_TooManyArgs_ShowsTerseUsageOnly(t *testing.T) {
	out := runCapture(t, "hierarchy", "ACME-1", "ACME-2")

	if strings.Contains(out, "Map an issue's place in the work hierarchy") {
		t.Errorf("too-many-args should not print the long description, got:\n%s", out)
	}
	if !strings.Contains(out, "Usage:") {
		t.Errorf("expected usage block for too-many-args, got:\n%s", out)
	}
}

// A second command with required args exercises the tool-wide nature of the
// behavior.
func TestRun_MissingRequiredArg_EditStatus(t *testing.T) {
	out := runCapture(t, "edit", "status")

	if !strings.Contains(out, "Move one or more issues through a workflow transition") {
		t.Errorf("expected full help for bare `edit status`, got:\n%s", out)
	}
}
