package discovery

import (
	"testing"
	"time"

	"p2p-transfer/internal/models"
)

func TestMarkStaleDevicesOfflineAndListOnlyActiveDevices(t *testing.T) {
	oldDevices := devices
	devices = make(map[string]models.Device)
	t.Cleanup(func() {
		devices = oldDevices
	})

	now := time.Now()
	devicesMu.Lock()
	devices["fresh"] = models.Device{ID: "fresh", Status: "online", LastSeen: now}
	devices["stale"] = models.Device{ID: "stale", Status: "online", LastSeen: now.Add(-offlineAfter - time.Second)}
	devices["already-offline"] = models.Device{ID: "already-offline", Status: "offline", LastSeen: now}
	devicesMu.Unlock()

	if !markStaleDevicesOffline(now) {
		t.Fatal("expected stale devices to update the device list")
	}

	active := GetActiveDevices()
	if len(active) != 1 || active[0].ID != "fresh" {
		t.Fatalf("GetActiveDevices() = %#v, want only fresh device", active)
	}

	devicesMu.RLock()
	defer devicesMu.RUnlock()
	if got := devices["stale"].Status; got != "offline" {
		t.Fatalf("stale device status = %q, want offline", got)
	}
}

func TestMarkStaleDevicesOfflineKeepsDeviceWithinTimeout(t *testing.T) {
	oldDevices := devices
	devices = make(map[string]models.Device)
	t.Cleanup(func() {
		devices = oldDevices
	})

	now := time.Now()
	devicesMu.Lock()
	devices["fresh"] = models.Device{ID: "fresh", Status: "online", LastSeen: now.Add(-offlineAfter)}
	devicesMu.Unlock()

	if markStaleDevicesOffline(now) {
		t.Fatal("device at the timeout boundary should remain online")
	}
	if active := GetActiveDevices(); len(active) != 1 || active[0].ID != "fresh" {
		t.Fatalf("GetActiveDevices() = %#v, want fresh device", active)
	}
}
