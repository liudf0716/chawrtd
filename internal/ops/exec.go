package ops

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var ifacePattern = regexp.MustCompile(`^[a-zA-Z0-9_.@-]+$`)

func RunShell(timeout time.Duration, script string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-lc", script)
	output, err := cmd.CombinedOutput()
	out := strings.TrimSpace(string(output))

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return out, fmt.Errorf("command timed out after %s", timeout)
	}
	if err != nil {
		if out == "" {
			return out, err
		}
		return out, fmt.Errorf("%w: %s", err, out)
	}
	return out, nil
}

func ShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func ValidateInterfaceName(value string) error {
	if !ifacePattern.MatchString(value) {
		return fmt.Errorf("invalid interface name: %s", value)
	}
	return nil
}

func ValidateIPv4(value string) error {
	ip := net.ParseIP(value)
	if ip == nil || ip.To4() == nil {
		return fmt.Errorf("invalid IPv4 address: %s", value)
	}
	return nil
}

func ValidateCIDR(value string) error {
	if _, _, err := net.ParseCIDR(value); err != nil {
		return fmt.Errorf("invalid CIDR: %s", value)
	}
	return nil
}
