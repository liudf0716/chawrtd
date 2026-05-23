package ops

import (
	"strings"
	"testing"
	"time"
)

func TestGetFRPSStatusUsesPermissionSafeConfigRead(t *testing.T) {
	originalRunShell := runShell
	originalGetVpsPublicIP := getVpsPublicIP
	t.Cleanup(func() {
		runShell = originalRunShell
		getVpsPublicIP = originalGetVpsPublicIP
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
	getVpsPublicIP = func(timeout time.Duration) (Result, error) {
		return Result{
			Summary: "Detected VPS public IPv4 address",
			Output:  "203.0.113.42",
			Data: map[string]any{
				"publicIp": "203.0.113.42",
			},
		}, nil
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
	if got, ok := result.Data["bindPort"].(int); !ok || got != 7070 {
		t.Fatalf("expected bindPort 7070 in data, got %#v", result.Data["bindPort"])
	}
	if got, ok := result.Data["token"].(string); !ok || got != "secret-token" {
		t.Fatalf("expected token in data, got %#v", result.Data["token"])
	}
	if got, ok := result.Data["publicIp"].(string); !ok || got != "203.0.113.42" {
		t.Fatalf("expected publicIp in data, got %#v", result.Data["publicIp"])
	}
	if got, ok := result.Data["configExists"].(bool); !ok || !got {
		t.Fatalf("expected configExists true in data, got %#v", result.Data["configExists"])
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

func TestVerifyFRPSDetectsTcpListener(t *testing.T) {
	originalRunShell := runShell
	t.Cleanup(func() {
		runShell = originalRunShell
	})

	var capturedScript string
	runShell = func(timeout time.Duration, script string) (string, error) {
		capturedScript = script
		return strings.Join([]string{
			"STATUS=LISTENING",
			"PROTOCOL=tcp",
			"PORT=7070",
			"MATCHES_BEGIN",
			"tcp   LISTEN 0      4096   0.0.0.0:7070   0.0.0.0:*",
			"MATCHES_END",
		}, "\n"), nil
	}

	result, err := VerifyFRPS(VerifyFRPSRequest{Protocol: "tcp", Port: 7070}, time.Second)
	if err != nil {
		t.Fatalf("VerifyFRPS returned error: %v", err)
	}

	if !strings.Contains(capturedScript, "ss -lntH \"sport = :7070\"") {
		t.Fatalf("expected tcp ss filter in script, got: %s", capturedScript)
	}
	if result.Summary != "Intranet-penetration service listener is active" {
		t.Fatalf("unexpected summary: %s", result.Summary)
	}
	if !strings.Contains(result.Output, "STATUS=LISTENING") {
		t.Fatalf("expected listening output, got: %s", result.Output)
	}
}

func TestVerifyFRPSRejectsInvalidProtocol(t *testing.T) {
	if _, err := VerifyFRPS(VerifyFRPSRequest{Protocol: "icmp", Port: 7070}, time.Second); err == nil {
		t.Fatal("expected error for invalid protocol")
	}
}
