package ops

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

var runShell = RunShell
var getVpsPublicIP = GetVpsPublicIP

type DeployFRPSRequest struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
}

type VerifyFRPSRequest struct {
	Protocol string `json:"protocol"`
	Port     int    `json:"port"`
}

type frpsServerConfig struct {
	BindPort int `toml:"bindPort"`
	Auth     struct {
		Method string `toml:"method"`
		Token  string `toml:"token"`
	} `toml:"auth"`
}

func parseFRPSServerConfig(content string) (frpsServerConfig, error) {
	var cfg frpsServerConfig
	if strings.TrimSpace(content) == "" {
		return cfg, nil
	}
	if err := toml.Unmarshal([]byte(content), &cfg); err != nil {
		return frpsServerConfig{}, err
	}
	return cfg, nil
}

func extractOutputSection(output, beginMarker, endMarker string) string {
	start := strings.Index(output, beginMarker)
	if start < 0 {
		return ""
	}
	start += len(beginMarker)
	end := strings.Index(output[start:], endMarker)
	if end < 0 {
		return strings.TrimSpace(output[start:])
	}
	return strings.TrimSpace(output[start : start+end])
}

func extractOutputValue(output, prefix string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func DeployFRPS(req DeployFRPSRequest, timeout time.Duration) (Result, error) {
	if req.Port <= 0 || req.Port > 65535 {
		return Result{}, fmt.Errorf("port must be between 1 and 65535")
	}
	if strings.TrimSpace(req.Token) == "" {
		return Result{}, fmt.Errorf("token is required")
	}

	token := strings.TrimSpace(req.Token)
	script := fmt.Sprintf(`set -euo pipefail
arch=$(uname -m)
case "$arch" in
  x86_64) frp_arch=amd64 ;;
  aarch64|arm64) frp_arch=arm64 ;;
  *) echo "unsupported architecture: $arch"; exit 1 ;;
esac

if ! command -v nwct-server >/dev/null 2>&1; then
  tmpdir=$(mktemp -d)
  trap 'rm -rf "$tmpdir"' EXIT
  curl -fsSL "https://github.com/fatedier/frp/releases/latest/download/frp_linux_${frp_arch}.tar.gz" -o "$tmpdir/frp.tar.gz"
  tar -xzf "$tmpdir/frp.tar.gz" -C "$tmpdir"
  bin=$(find "$tmpdir" -type f -name frps | head -n1)
  if [ -z "$bin" ]; then
    echo "frps binary not found in downloaded archive"
    exit 1
  fi
  sudo install -m 755 "$bin" /usr/bin/nwct-server
fi

sudo mkdir -p /etc/nwct
cat <<'EOF' | sudo tee /etc/nwct/nwct-server.toml >/dev/null
bindPort = %d
auth.method = "token"
auth.token = %q
EOF

cat <<'EOF' | sudo tee /etc/systemd/system/nwct-server.service >/dev/null
[Unit]
Description=NWCT Server (FRPS)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/bin/nwct-server -c /etc/nwct/nwct-server.toml
Restart=always
RestartSec=2
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now nwct-server
sudo systemctl status --no-pager --lines=20 nwct-server || true
`, req.Port, token)

	output, err := runShell(timeout, script)
	if err != nil {
		return Result{}, err
	}

	return Result{
		Summary: "FRPS deployed successfully",
		Output:  output,
		Data: map[string]any{
			"service": "nwct-server",
			"port":    req.Port,
		},
	}, nil
}

func GetFRPSStatus(timeout time.Duration) (Result, error) {
	configPath := "/etc/nwct/nwct-server.toml"
	script := `set -euo pipefail
config="/etc/nwct/nwct-server.toml"
service_state=$(systemctl is-active nwct-server 2>/dev/null || true)
if [ -z "$service_state" ]; then
  service_state="unknown"
fi

read_config() {
	if [ -r "$1" ]; then
		cat "$1"
		return 0
	fi
	sudo -n cat "$1" 2>/dev/null || true
}

cfg=$(read_config "$config")

ports=$(ss -lntup 2>/dev/null | grep -E "(nwct-server|frps)" || true)

echo "SERVICE_STATE=$service_state"
echo "CONFIG_EXISTS=$([ -f "$config" ] && echo yes || echo no)"
echo "CONFIG_BEGIN"
echo "$cfg"
echo "CONFIG_END"
echo "PORTS_BEGIN"
echo "$ports"
echo "PORTS_END"
`

	output, err := runShell(timeout, script)
	if err != nil {
		return Result{}, err
	}

	configSection := extractOutputSection(output, "CONFIG_BEGIN", "CONFIG_END")
	portsSection := extractOutputSection(output, "PORTS_BEGIN", "PORTS_END")
	serviceState := extractOutputValue(output, "SERVICE_STATE=")
	configExists := strings.EqualFold(extractOutputValue(output, "CONFIG_EXISTS="), "yes")
	parsedConfig, err := parseFRPSServerConfig(configSection)
	if err != nil {
		return Result{}, fmt.Errorf("failed to parse nwct server config: %w", err)
	}

	publicIPResult, publicIPErr := getVpsPublicIP(timeout)
	publicIP := ""
	if publicIPErr == nil {
		if data := publicIPResult.Data; data != nil {
			if value, ok := data["publicIp"].(string); ok {
				publicIP = strings.TrimSpace(value)
			}
		}
	}

	data := map[string]any{
		"serviceState": serviceState,
		"configExists": configExists,
		"configPath":   configPath,
		"bindPort":     parsedConfig.BindPort,
		"token":        parsedConfig.Auth.Token,
		"ports":        portsSection,
	}
	if publicIP != "" {
		data["publicIp"] = publicIP
		data["publicIpSource"] = "curl https://ifconfig.me/ip"
	}
	if publicIPErr != nil {
		data["publicIpError"] = publicIPErr.Error()
	}

	resultOutput := redactFRPSToken(output)
	if publicIP != "" {
		resultOutput += "\nPUBLIC_IP=" + publicIP
	}
	if publicIPErr != nil {
		resultOutput += "\nPUBLIC_IP_ERROR=" + publicIPErr.Error()
	}

	return Result{
		Summary: "Fetched FRPS status",
		Output:  resultOutput,
		Data:    data,
	}, nil
}

func VerifyFRPS(req VerifyFRPSRequest, timeout time.Duration) (Result, error) {
	proto := strings.ToLower(strings.TrimSpace(req.Protocol))
	if proto != "tcp" && proto != "udp" {
		return Result{}, fmt.Errorf("protocol must be tcp or udp")
	}
	if req.Port <= 0 || req.Port > 65535 {
		return Result{}, fmt.Errorf("port must be between 1 and 65535")
	}

	script := fmt.Sprintf(`set -euo pipefail
proto=%s
port=%d

case "$proto" in
  tcp)
    output=$(ss -lntH "sport = :%d" 2>/dev/null || true)
    ;;
  udp)
    output=$(ss -lnuH "sport = :%d" 2>/dev/null || true)
    ;;
esac

if [ -n "$output" ]; then
  echo "STATUS=LISTENING"
  echo "PROTOCOL=$proto"
  echo "PORT=$port"
  echo "MATCHES_BEGIN"
  echo "$output"
  echo "MATCHES_END"
else
  echo "STATUS=NOT_LISTENING"
  echo "PROTOCOL=$proto"
  echo "PORT=$port"
fi
`, ShellQuote(proto), req.Port, req.Port, req.Port)

	output, err := runShell(timeout, script)
	if err != nil {
		return Result{}, err
	}

	listening := strings.Contains(output, "STATUS=LISTENING")
	summary := "Intranet-penetration service listener not found"
	if listening {
		summary = "Intranet-penetration service listener is active"
	}

	return Result{
		Summary: summary,
		Output:  output,
		Data: map[string]any{
			"protocol":  proto,
			"port":      req.Port,
			"listening": listening,
		},
	}, nil
}

func ResetFRPS(timeout time.Duration) (Result, error) {
	script := `set -euo pipefail
sudo systemctl stop nwct-server || true
sudo systemctl disable nwct-server || true
sudo rm -f /etc/systemd/system/nwct-server.service
sudo rm -rf /etc/nwct
sudo systemctl daemon-reload
`
	output, err := runShell(timeout, script)
	if err != nil {
		return Result{}, err
	}

	return Result{
		Summary: "FRPS reset completed",
		Output:  output,
	}, nil
}

func redactFRPSToken(content string) string {
	re := regexp.MustCompile(`(?m)^(auth\.token\s*=\s*).+$`)
	return re.ReplaceAllString(content, `$1"[REDACTED]"`)
}
