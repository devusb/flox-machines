package machines

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"storage":"image","persistSize":512,"reservedNames":["guestonly"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACHINE_CONFIG", path)
	c, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if c.StateDir != "/var/lib/microvms" || c.LeasesPath != "/var/lib/dnsmasq/dnsmasq.leases" {
		t.Errorf("defaults not applied: %+v", c)
	}
	if c.Storage != "image" || c.PersistSize != 512 || len(c.ReservedNames) != 1 || c.ReservedNames[0] != "guestonly" {
		t.Errorf("fields not read: %+v", c)
	}
}
