package ops

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

var wireGuardPublicKeyPattern = regexp.MustCompile(`^[A-Za-z0-9+/]{43}=$`)

type DeployWireGuardRequest struct {
	Port            int                          `json:"port"`
	TunnelIP        string                       `json:"tunnelIp"`
	EgressInterface string                       `json:"egressInterface"`
	PeerBindings    []DeployWireGuardPeerBinding `json:"peerBindings"`
}

type DeployWireGuardPeerBinding struct {
	DeviceID      string `json:"deviceId"`
	PeerPublicKey string `json:"peerPublicKey"`
	TunnelIP      string `json:"tunnelIp"`
	LanCIDR       string `json:"lanCidr"`
	Endpoint      string `json:"endpoint"`
}

type ResetWireGuardRequest struct {
	Interface  string `json:"interface"`
	RemoveKeys bool   `json:"removeKeys"`
}

type VerifyWireGuardRequest struct {
	PingTargets []string `json:"pingTargets"`
}

type wireGuardServerPeer struct {
	PublicKey  string   `json:"publicKey"`
	AllowedIPs []string `json:"allowedIps"`
}

func parseWGStatusOutput(output string) (wgShow string, natRules string, ipForwardOk bool, snatOk bool) {
	lines := strings.Split(output, "\n")
	inWG := false
	inNAT := false
	var wgBuilder strings.Builder
	var natBuilder strings.Builder

	for _, line := range lines {
		switch strings.TrimSpace(line) {
		case "WG_BEGIN":
			inWG = true
			inNAT = false
			continue
		case "WG_END":
			inWG = false
			continue
		case "NAT_BEGIN":
			inNAT = true
			inWG = false
			continue
		case "NAT_END":
			inNAT = false
			continue
		}

		if strings.HasPrefix(line, "IP_FORWARD=") {
			raw := strings.TrimSpace(strings.TrimPrefix(line, "IP_FORWARD="))
			ipForwardOk = raw == "1"
			continue
		}

		if inWG {
			wgBuilder.WriteString(line)
			wgBuilder.WriteString("\n")
		}
		if inNAT {
			natBuilder.WriteString(line)
			natBuilder.WriteString("\n")
		}
	}

	wgShow = strings.TrimSpace(wgBuilder.String())
	natRules = strings.TrimSpace(natBuilder.String())
	snatOk = strings.Contains(natRules, "-j MASQUERADE")
	return wgShow, natRules, ipForwardOk, snatOk
}

func parseWireGuardPeers(configContent string) []wireGuardServerPeer {
	peers := make([]wireGuardServerPeer, 0)
	lines := strings.Split(configContent, "\n")

	current := wireGuardServerPeer{}
	inPeer := false
	flush := func() {
		if !inPeer {
			return
		}
		if current.PublicKey != "" {
			peers = append(peers, current)
		}
		current = wireGuardServerPeer{}
		inPeer = false
	}

	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if line == "[Peer]" {
			flush()
			inPeer = true
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			flush()
			continue
		}
		if !inPeer {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(strings.ToLower(parts[0]))
		value := strings.TrimSpace(parts[1])
		switch key {
		case "publickey":
			current.PublicKey = value
		case "allowedips":
			entries := strings.Split(value, ",")
			allowed := make([]string, 0, len(entries))
			for _, entry := range entries {
				normalized := strings.TrimSpace(entry)
				if normalized != "" {
					allowed = append(allowed, normalized)
				}
			}
			current.AllowedIPs = allowed
		}
	}
	flush()
	return peers
}

func buildWireGuardServerReportLines(ipForwardOk bool, snatOk bool, keyCheck map[string]any) []string {
	lines := []string{
		fmt.Sprintf("- IP Forwarding: %s", boolLabel(ipForwardOk, "enabled", "disabled")),
		fmt.Sprintf("- SNAT/MASQUERADE: %s", boolLabel(snatOk, "present", "missing")),
	}

	status, _ := keyCheck["status"].(string)
	switch status {
	case "ok":
		lines = append(lines, "- Server key pair: ✅ private/public key match")
	case "mismatch":
		lines = append(lines, "- Server key pair: ❌ configured public key does not match the derived public key from server_private.key")
	case "error":
		if errText, ok := keyCheck["error"].(string); ok && errText != "" {
			lines = append(lines, fmt.Sprintf("- Server key pair: ❌ check failed (%s)", errText))
		} else {
			lines = append(lines, "- Server key pair: ❌ check failed")
		}
	}

	return lines
}

func boolLabel(ok bool, yes string, no string) string {
	if ok {
		return "✅ " + yes
	}
	return "❌ " + no
}

func DeployWireGuard(req DeployWireGuardRequest, timeout time.Duration) (Result, error) {
	port := req.Port
	if port == 0 {
		port = 51820
	}
	if port < 1 || port > 65535 {
		return Result{}, fmt.Errorf("port must be between 1 and 65535")
	}

	tunnelIP := strings.TrimSpace(req.TunnelIP)
	if tunnelIP == "" {
		tunnelIP = "10.0.0.1/24"
	}
	if err := ValidateCIDR(tunnelIP); err != nil {
		return Result{}, err
	}

	egress := strings.TrimSpace(req.EgressInterface)
	if egress != "" {
		if err := ValidateInterfaceName(egress); err != nil {
			return Result{}, err
		}
	}

	egressAssign := ""
	if egress == "" {
		egressAssign = `egress_if=$(ip route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="dev") {print $(i+1); exit}}')
if [ -z "$egress_if" ]; then
  egress_if=$(ip -4 route show default 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="dev") {print $(i+1); exit}}')
fi
if [ -z "$egress_if" ]; then
  echo "failed to auto-detect egress interface"
  exit 1
fi`
	} else {
		egressAssign = fmt.Sprintf("egress_if=%s", ShellQuote(egress))
	}

	peerBlocks := ""
	normalizedPeers := make([]map[string]string, 0, len(req.PeerBindings))
	for _, rawPeer := range req.PeerBindings {
		peerPublicKey := strings.TrimSpace(rawPeer.PeerPublicKey)
		if !wireGuardPublicKeyPattern.MatchString(peerPublicKey) {
			return Result{}, fmt.Errorf("invalid peer public key for %s", strings.TrimSpace(rawPeer.DeviceID))
		}

		tunnelCIDR := strings.TrimSpace(rawPeer.TunnelIP)
		if err := ValidateCIDR(tunnelCIDR); err != nil {
			return Result{}, fmt.Errorf("invalid peer tunnelIp for %s: %w", strings.TrimSpace(rawPeer.DeviceID), err)
		}

		lanCIDR := strings.TrimSpace(rawPeer.LanCIDR)
		if err := ValidateCIDR(lanCIDR); err != nil {
			return Result{}, fmt.Errorf("invalid peer lanCidr for %s: %w", strings.TrimSpace(rawPeer.DeviceID), err)
		}

		endpoint := strings.TrimSpace(rawPeer.Endpoint)
		allowed := tunnelCIDR + ", " + lanCIDR
		endpointLine := ""
		if endpoint != "" {
			endpointLine = "\nEndpoint = " + endpoint
		}

		peerBlocks += "\n[Peer]\n"
		peerBlocks += "PublicKey = " + peerPublicKey + "\n"
		peerBlocks += "AllowedIPs = " + allowed + endpointLine + "\n"

		normalizedPeers = append(normalizedPeers, map[string]string{
			"deviceId":  strings.TrimSpace(rawPeer.DeviceID),
			"tunnelIp":  tunnelCIDR,
			"lanCidr":   lanCIDR,
			"endpoint":  endpoint,
			"publicKey": peerPublicKey,
		})
	}

	script := fmt.Sprintf(`set -euo pipefail
sudo apt-get update -y >/dev/null
sudo apt-get install -y wireguard iptables curl >/dev/null

sudo sysctl -w net.ipv4.ip_forward=1 >/dev/null
sudo sh -c 'grep -q "^net.ipv4.ip_forward=1$" /etc/sysctl.conf || echo net.ipv4.ip_forward=1 >> /etc/sysctl.conf'

%s

sudo mkdir -p /etc/wireguard
if [ ! -s /etc/wireguard/server_private.key ] || [ ! -s /etc/wireguard/server_public.key ]; then
  sudo sh -c 'umask 077; wg genkey | tee /etc/wireguard/server_private.key | wg pubkey > /etc/wireguard/server_public.key'
fi

private_key=$(sudo cat /etc/wireguard/server_private.key)

cat <<EOF | sudo tee /etc/wireguard/wg0.conf >/dev/null
[Interface]
Address = %s
ListenPort = %d
PrivateKey = $private_key
PostUp = iptables -t nat -A POSTROUTING -m comment --comment OPENCLAW_WG_wg0 -o $egress_if -j MASQUERADE; iptables -A FORWARD -i wg0 -j ACCEPT; iptables -A FORWARD -o wg0 -j ACCEPT
PostDown = iptables -t nat -D POSTROUTING -m comment --comment OPENCLAW_WG_wg0 -o $egress_if -j MASQUERADE; iptables -D FORWARD -i wg0 -j ACCEPT; iptables -D FORWARD -o wg0 -j ACCEPT
%s
EOF

sudo chmod 600 /etc/wireguard/wg0.conf
sudo systemctl daemon-reload
sudo systemctl enable --now wg-quick@wg0

server_pub=$(sudo cat /etc/wireguard/server_public.key)
echo "SERVER_PUBLIC_KEY=$server_pub"
echo "EGRESS_IF=$egress_if"
`, egressAssign, tunnelIP, port, strings.TrimSpace(peerBlocks))

	output, err := RunShell(timeout, script)
	if err != nil {
		return Result{}, err
	}

	return Result{
		Summary: "WireGuard server deployed successfully",
		Output:  output,
		Data: map[string]any{
			"port":         port,
			"tunnelIp":     tunnelIP,
			"peerBindings": normalizedPeers,
		},
	}, nil
}

func GetWireGuardStatus(timeout time.Duration) (Result, error) {
	script := `set -euo pipefail
wg_out=$(sudo wg show 2>&1 || true)
nat_out=$(sudo iptables -t nat -S POSTROUTING 2>&1 || true)
forward=$(sysctl -n net.ipv4.ip_forward 2>/dev/null || cat /proc/sys/net/ipv4/ip_forward 2>/dev/null || echo unknown)

echo "WG_BEGIN"
echo "$wg_out"
echo "WG_END"
echo "NAT_BEGIN"
echo "$nat_out"
echo "NAT_END"
echo "IP_FORWARD=$forward"
`
	output, err := RunShell(timeout, script)
	if err != nil {
		return Result{}, err
	}

	wgShow, natRules, ipForwardOk, snatOk := parseWGStatusOutput(output)

	serverPublicKey := ""
	if out, keyErr := RunShell(timeout, "sudo cat /etc/wireguard/server_public.key 2>/dev/null || true"); keyErr == nil {
		serverPublicKey = strings.TrimSpace(out)
	}

	keyCheck := map[string]any{
		"status": "skipped",
	}
	if serverPublicKey != "" {
		derivedServerPublicKey := ""
		if out, deriveErr := RunShell(timeout, "sudo sh -c 'cat /etc/wireguard/server_private.key 2>/dev/null | wg pubkey 2>/dev/null || true'"); deriveErr == nil {
			derivedServerPublicKey = strings.TrimSpace(out)
		}
		if derivedServerPublicKey != "" {
			status := "mismatch"
			if derivedServerPublicKey == serverPublicKey {
				status = "ok"
			}
			keyCheck = map[string]any{
				"status":              status,
				"configuredPublicKey": serverPublicKey,
				"derivedPublicKey":    derivedServerPublicKey,
			}
		} else {
			keyCheck = map[string]any{
				"status": "error",
				"error":  "failed to derive server public key from private key",
			}
		}
	}

	return Result{
		Summary: "Fetched WireGuard server status",
		Output:  output,
		Data: map[string]any{
			"server": map[string]any{
				"wgShow":          wgShow,
				"natRules":        natRules,
				"ipForwardOk":     ipForwardOk,
				"snatOk":          snatOk,
				"serverPublicKey": serverPublicKey,
				"keyCheck":        keyCheck,
				"reportLines":     buildWireGuardServerReportLines(ipForwardOk, snatOk, keyCheck),
			},
		},
	}, nil
}

func GetVpsPublicIP(timeout time.Duration) (Result, error) {
	output, err := RunShell(timeout, `set -euo pipefail
for url in \
  https://ifconfig.me/ip \
  https://api.ipify.org \
  https://checkip.amazonaws.com
do
  public_ip=$(curl -4 -fsSL --max-time 8 "$url" | tr -d '[:space:]')
  if [ -n "$public_ip" ]; then
    printf 'PUBLIC_IP=%s\nSOURCE=%s\n' "$public_ip" "$url"
    exit 0
  fi
done
exit 1`)
	if err != nil {
		return Result{}, err
	}

	publicIP := extractOutputValue(output, "PUBLIC_IP=")
	source := extractOutputValue(output, "SOURCE=")
	if publicIP == "" {
		return Result{}, fmt.Errorf("public IP detection returned an empty response")
	}
	if err := ValidateIPv4(publicIP); err != nil {
		return Result{}, fmt.Errorf("public IP detection returned a non-IPv4 response: %s", publicIP)
	}
	if source == "" {
		source = "curl fallback public-ip probes"
	}

	return Result{
		Summary: "Detected VPS public IPv4 address",
		Output:  publicIP,
		Data: map[string]any{
			"publicIp": publicIP,
			"source":   source,
		},
	}, nil
}

func ResetWireGuard(req ResetWireGuardRequest, timeout time.Duration) (Result, error) {
	iface := strings.TrimSpace(req.Interface)
	if iface == "" {
		iface = "wg0"
	}
	if err := ValidateInterfaceName(iface); err != nil {
		return Result{}, err
	}

	removeKeys := req.RemoveKeys
	rmKeys := ""
	if removeKeys {
		rmKeys = "sudo rm -f /etc/wireguard/server_private.key /etc/wireguard/server_public.key"
	}

	script := fmt.Sprintf(`set -euo pipefail
comment="OPENCLAW_WG_%s"
sudo systemctl stop wg-quick@%s || true
sudo systemctl disable wg-quick@%s || true
sudo rm -f /etc/wireguard/%s.conf

while true; do
  rule=$(sudo iptables -t nat -S POSTROUTING 2>/dev/null | grep -- "--comment $comment" | head -n1 | sed 's/^-A /-D /' || true)
  if [ -z "$rule" ]; then
    break
  fi
  sudo iptables -t nat $rule || true
done

%s
`, iface, iface, iface, iface, rmKeys)

	output, err := RunShell(timeout, script)
	if err != nil {
		return Result{}, err
	}

	return Result{
		Summary: "WireGuard server reset completed",
		Output:  output,
		Data: map[string]any{
			"interface":  iface,
			"removeKeys": removeKeys,
		},
	}, nil
}

func VerifyWireGuardServer(req VerifyWireGuardRequest, timeout time.Duration) (Result, error) {
	status, err := GetWireGuardStatus(timeout)
	if err != nil {
		return Result{}, err
	}

	wgShow, natRules, ipForwardOk, snatOk := parseWGStatusOutput(status.Output)

	serverPublicKey := ""
	if out, keyErr := RunShell(timeout, "sudo cat /etc/wireguard/server_public.key 2>/dev/null || true"); keyErr == nil {
		serverPublicKey = strings.TrimSpace(out)
	}

	keyCheck := map[string]any{
		"status": "skipped",
	}
	if serverPublicKey != "" {
		derivedServerPublicKey := ""
		if out, deriveErr := RunShell(timeout, "sudo sh -c 'cat /etc/wireguard/server_private.key 2>/dev/null | wg pubkey 2>/dev/null || true'"); deriveErr == nil {
			derivedServerPublicKey = strings.TrimSpace(out)
		}
		if derivedServerPublicKey != "" {
			status := "mismatch"
			if derivedServerPublicKey == serverPublicKey {
				status = "ok"
			}
			keyCheck = map[string]any{
				"status":              status,
				"configuredPublicKey": serverPublicKey,
				"derivedPublicKey":    derivedServerPublicKey,
			}
		} else {
			keyCheck = map[string]any{
				"status": "error",
				"error":  "failed to derive server public key from private key",
			}
		}
	}

	serverPeerConfig := make([]wireGuardServerPeer, 0)
	if out, confErr := RunShell(timeout, "sudo cat /etc/wireguard/wg0.conf 2>/dev/null || true"); confErr == nil {
		serverPeerConfig = parseWireGuardPeers(out)
	}

	results := make([]map[string]any, 0, len(req.PingTargets))
	for _, target := range req.PingTargets {
		t := strings.TrimSpace(target)
		if t == "" {
			continue
		}
		if err := ValidateIPv4(t); err != nil {
			return Result{}, err
		}

		routeOutput, _ := RunShell(5*time.Second, "ip -4 route get "+ShellQuote(t)+" 2>/dev/null || true")
		routeViaWG0 := strings.Contains(routeOutput, " dev wg0")

		reachable := false
		finalOutput := ""
		if routeViaWG0 {
			for i := 0; i < 5; i++ {
				out, pingErr := RunShell(5*time.Second, "ping -I wg0 -c 1 -W 1 "+ShellQuote(t))
				finalOutput = out
				if pingErr == nil {
					reachable = true
					break
				}
			}
		}

		confidence := "failed"
		message := "target is not routed via wg0"
		if reachable {
			confidence = "confirmed"
			message = "ICMP echo reply received via wg0"
		} else if routeViaWG0 {
			confidence = "inconclusive"
			message = "route via wg0 exists but ICMP echo failed; target may block ping"
		}

		results = append(results, map[string]any{
			"target":      t,
			"reachable":   reachable,
			"output":      finalOutput,
			"route":       strings.TrimSpace(routeOutput),
			"routeViaWg0": routeViaWG0,
			"confidence":  confidence,
			"message":     message,
		})
	}

	return Result{
		Summary: "Verified WireGuard server connectivity",
		Output:  status.Output,
		Data: map[string]any{
			"server": map[string]any{
				"wgShow":          wgShow,
				"natRules":        natRules,
				"ipForwardOk":     ipForwardOk,
				"snatOk":          snatOk,
				"serverPublicKey": serverPublicKey,
				"keyCheck":        keyCheck,
				"peerConfig":      serverPeerConfig,
				"reportLines":     buildWireGuardServerReportLines(ipForwardOk, snatOk, keyCheck),
			},
			"pingResults": results,
		},
	}, nil
}
