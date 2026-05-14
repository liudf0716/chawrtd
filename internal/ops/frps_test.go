package ops

import (
	"strings"
	"testing"
	"time"
)

func TestGetFRPSStatusUsesPermissionSafeConfigRead(t *testing.T) {
	originalRunShell := runShell
	t.Cleanup(func() {
		runShell = originalRunShell
	})

	var capturedScript string
	runShell = func(timeout time.Duration, script string) (string, error) {
		capturedScript = script
		return strings.Join([]string{
			"SERVICE_STATE=active",
			"CONFIG_EXISTS=yes",
			"CONFIG_BEGIN",
			"bindPort = 7070",
			"auth.method = \"token\"",
			"auth.token = \"secret-token\"",
			"CONFIG_END",
			"PORTS_BEGIN",
			"tcp    LISTEN 0      4096         0.0.0.0:7070      0.0.0.0:*    users:((\"nwct-server\",pid=1,fd=3))",
			"PORTS_END",
		}, "\n"), nil
	}

	result, err := GetFRPSStatus(time.Second)
	if err != nil {
		t.Fatalf("GetFRPSStatus returned error: %v", err)
	}

	if !strings.Contains(capturedScript, "sudo -n cat \"$1\" 2>/dev/null || true") {
		t.Fatalf("expected permission-safe config read in script, got: %s", capturedScript)
	}
	if strings.Contains(result.Output, "secret-token") {
		t.Fatalf("expected token to be redacted, got output: %s", result.Output)
	}
	if !strings.Contains(result.Output, "[REDACTED]") {
		t.Fatalf("expected redacted token marker in output, got: %s", result.Output)
	}
}

func TestRedactFRPSToken(t *testing.T) {
	input := "auth.token = \"abc123\"\nother = value"
	got := redactFRPSToken(input)
	want := "auth.token = \"[REDACTED]\"\nother = value"

	if got != want {
		t.Fatalf("redactFRPSToken() = %q, want %q", got, want)
	}
}
