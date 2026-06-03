//go:build !windows

package systray

// Start is a no-op stub for non-windows builds.
func Start(onOpenDashboard, onSettings, onRestartDiscovery, onExit func()) {}

// ShowNotification is a no-op stub for non-windows builds.
func ShowNotification(title, message string) {}
