package machines

import (
	"encoding/json"
	"fmt"
	"os"
)

const DefaultConfigPath = "/etc/flox-machines/config.json"

type Config struct {
	StateDir      string   `json:"stateDir"`
	Storage       string   `json:"storage"`
	ParentDataset string   `json:"parentDataset"`
	PersistSize   int      `json:"persistSize"`
	KeyPath       string   `json:"keyPath"`
	ReservedNames []string `json:"reservedNames"`
	LeasesPath    string   `json:"leasesPath"`
}

func LoadConfig() (Config, error) {
	path := os.Getenv("MACHINE_CONFIG")
	if path == "" {
		path = DefaultConfigPath
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}
	if c.StateDir == "" {
		c.StateDir = "/var/lib/microvms"
	}
	if c.LeasesPath == "" {
		c.LeasesPath = "/var/lib/dnsmasq/dnsmasq.leases"
	}
	return c, nil
}
