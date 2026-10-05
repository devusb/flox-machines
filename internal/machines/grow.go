package machines

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (m *Manager) Grow(ctx context.Context, name, volume string, sizeMB int) error {
	if err := m.require(name); err != nil {
		return err
	}
	zvol := volume == "persist" && m.Config.Storage == "zfs"
	var dev string
	switch volume {
	case "persist":
		dev = filepath.Join(m.dir(name), "persist.img")
	case "store":
		dev = filepath.Join(m.dir(name), "nix-store-overlay.img")
	case "var":
		dev = filepath.Join(m.dir(name), "nix-var.img")
	default:
		return errors.New("usage: machine grow <name> persist|store|var <MB>")
	}
	var current int64
	if zvol {
		dev = "/dev/zvol/" + m.zvol(name)
		out, err := m.Runner.Run(ctx, "zfs", "get", "-Hp", "-o", "value", "volsize", m.zvol(name))
		if err != nil {
			return err
		}
		if current, err = strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64); err != nil {
			return err
		}
	} else {
		st, err := os.Stat(dev)
		if err != nil {
			return fmt.Errorf("machine '%s' has no %s volume yet", name, volume)
		}
		current = st.Size()
	}
	if int64(sizeMB)<<20 <= current {
		return fmt.Errorf("%s is already %d MB", volume, current>>20)
	}

	running := m.run(ctx, "systemctl", "is-active", "-q", unit(name)) == nil
	if err := m.run(ctx, "systemctl", "stop", unit(name)); err != nil {
		return err
	}
	if zvol {
		if err := m.run(ctx, "zfs", "set", fmt.Sprintf("volsize=%dM", sizeMB), m.zvol(name)); err != nil {
			return err
		}
		if err := m.run(ctx, "udevadm", "settle"); err != nil {
			return err
		}
	} else if err := os.Truncate(dev, int64(sizeMB)<<20); err != nil {
		return err
	}
	if err := m.run(ctx, "e2fsck", "-f", "-p", dev); err != nil {
		var exit *ExitError
		if !errors.As(err, &exit) || exit.Code > 1 {
			return fmt.Errorf("e2fsck on %s: %w", volume, err)
		}
	}
	if err := m.run(ctx, "resize2fs", dev); err != nil {
		return err
	}
	if running {
		return m.run(ctx, "systemctl", "start", unit(name))
	}
	return nil
}
