package browser

import (
	"errors"
	"reflect"
	"testing"
)

func TestBrowserCommandDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		goos string
		cmd  string
		args []string
	}{
		{name: "macOS", goos: "darwin", cmd: "open", args: []string{"https://example.test"}},
		{name: "Linux", goos: "linux", cmd: "xdg-open", args: []string{"https://example.test"}},
		{name: "Windows", goos: "windows", cmd: "rundll32.exe", args: []string{"url.dll,FileProtocolHandler", "https://example.test"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd, args, err := browserCommand(tt.goos, "", "https://example.test")
			if err != nil {
				t.Fatalf("browserCommand() error = %v", err)
			}
			if cmd != tt.cmd {
				t.Errorf("command = %q, want %q", cmd, tt.cmd)
			}
			if !reflect.DeepEqual(args, tt.args) {
				t.Errorf("args = %#v, want %#v", args, tt.args)
			}
		})
	}
}

func TestBrowserCommandOverrides(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		goos     string
		override string
		cmd      string
		args     []string
	}{
		{
			name:     "Linux Edge alias",
			goos:     "linux",
			override: "microsoft-edge",
			cmd:      "microsoft-edge",
			args:     []string{"https://example.test"},
		},
		{
			name:     "Windows Edge alias",
			goos:     "windows",
			override: "edge",
			cmd:      "msedge.exe",
			args:     []string{"https://example.test"},
		},
		{
			name:     "macOS Edge alias",
			goos:     "darwin",
			override: "edge",
			cmd:      "open",
			args:     []string{"-a", "Microsoft Edge", "https://example.test"},
		},
		{
			name:     "custom executable",
			goos:     "linux",
			override: "/opt/browser/custom-browser",
			cmd:      "/opt/browser/custom-browser",
			args:     []string{"https://example.test"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd, args, err := browserCommand(tt.goos, tt.override, "https://example.test")
			if err != nil {
				t.Fatalf("browserCommand() error = %v", err)
			}
			if cmd != tt.cmd {
				t.Errorf("command = %q, want %q", cmd, tt.cmd)
			}
			if !reflect.DeepEqual(args, tt.args) {
				t.Errorf("args = %#v, want %#v", args, tt.args)
			}
		})
	}
}

func TestBrowserCommandUnsupportedOS(t *testing.T) {
	t.Parallel()

	_, _, err := browserCommand("plan9", "", "https://example.test")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("browserCommand() error = %v, want ErrUnsupported", err)
	}
}
