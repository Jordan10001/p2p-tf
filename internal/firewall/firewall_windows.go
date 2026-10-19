//go:build windows

package firewall

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// EnsureTransferPort asks for elevation at every application startup so the
// user explicitly approves the private-LAN transfer rule each time.
func EnsureTransferPort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid transfer port %d", port)
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get application path: %w", err)
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return fmt.Errorf("encode application path: %w", err)
	}
	params, _ := windows.UTF16PtrFromString(fmt.Sprintf("--install-firewall-rule %d", port))
	if err := windows.ShellExecute(0, verb, file, params, nil, windows.SW_HIDE); err != nil {
		return fmt.Errorf("request Windows firewall permission: %w", err)
	}
	return nil
}

// InstallTransferPort is called by the elevated helper process.
func InstallTransferPort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid transfer port %d", port)
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get application path: %w", err)
	}
	name := firewallRuleName(exe, port)
	displayName := fmt.Sprintf("P2P Transfer (TCP %d, Local Network)", port)
	command := fmt.Sprintf(
		"Remove-NetFirewallRule -Name '%s' -ErrorAction SilentlyContinue; New-NetFirewallRule -Name '%s' -DisplayName '%s' -Group 'P2P Transfer' -Program '%s' -Direction Inbound -Action Allow -Enabled True -Profile Any -Protocol TCP -LocalPort %d -RemoteAddress LocalSubnet -ErrorAction Stop | Out-Null",
		name, name, displayName, quotePowerShell(exe), port,
	)
	output, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", command).CombinedOutput()
	if err != nil {
		return fmt.Errorf("create Windows firewall rule: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func firewallRuleName(exe string, port int) string {
	path := strings.ToLower(filepath.Clean(exe))
	sum := sha256.Sum256([]byte(path))
	return fmt.Sprintf("P2PTransfer-TCP-%d-%s", port, hex.EncodeToString(sum[:6]))
}

func quotePowerShell(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}
