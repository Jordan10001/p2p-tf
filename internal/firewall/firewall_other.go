//go:build !windows

package firewall

import "fmt"

func EnsureTransferPort(port int) error { return nil }

func InstallTransferPort(port int) error {
	return fmt.Errorf("firewall setup is only supported on Windows")
}
