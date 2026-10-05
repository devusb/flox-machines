package machines

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func (m *Manager) Status(ctx context.Context, name string) (Status, error) {
	if err := ValidName(name); err != nil {
		return Status{}, err
	}
	d := m.dir(name)
	if st, err := os.Stat(d); err != nil || !st.IsDir() {
		return Status{Name: name}, nil
	}
	s := Status{Name: name, Exists: true}
	owner, err := os.ReadFile(filepath.Join(d, "owner"))
	if err == nil {
		s.Owner = strings.TrimSpace(string(owner))
	} else if !os.IsNotExist(err) {
		return Status{}, err
	}
	s.Running = m.run(ctx, "systemctl", "is-active", "-q", unit(name)) == nil
	if _, err := m.runSSH(ctx, 5*time.Second, name, "true"); err == nil {
		s.Reachable = true
		if raw, err := m.runSSH(ctx, 10*time.Second, name, "tailscale", "status", "--json"); err == nil && len(raw) > 0 {
			if ts, err := ParseTailscaleStatus(raw); err == nil {
				s.Tailscale = &ts
			}
		}
	}
	return s, nil
}

func (m *Manager) Login(ctx context.Context, name string) error {
	if err := m.require(name); err != nil {
		return err
	}
	if raw, err := m.runSSH(ctx, 10*time.Second, name, "tailscale", "status", "--json"); err == nil {
		if ts, err := ParseTailscaleStatus(raw); err == nil && ts.State == "Running" {
			return nil
		}
	}
	if _, err := m.runSSH(ctx, 15*time.Second, name, "systemctl stop machine-tailscale-login.service 2> /dev/null; systemd-run --quiet --collect --unit=machine-tailscale-login tailscale up --ssh"); err != nil {
		return fmt.Errorf("could not start a Tailscale login on machine '%s'", name)
	}
	return nil
}

func (m *Manager) Restart(ctx context.Context, name string) error {
	if err := m.require(name); err != nil {
		return err
	}
	return m.run(ctx, "systemctl", "restart", unit(name))
}

func (m *Manager) setSize(name string, lines []string) error {
	p := filepath.Join(m.dir(name), "instance.env")
	env, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	var kept []string
	for _, line := range strings.Split(strings.TrimRight(string(env), "\n"), "\n") {
		if !strings.HasPrefix(line, "MICROVM_MEM=") && !strings.HasPrefix(line, "MICROVM_VCPU=") {
			kept = append(kept, line)
		}
	}
	kept = append(kept, lines...)
	return os.WriteFile(p, []byte(strings.Join(kept, "\n")+"\n"), 0o644)
}

func (m *Manager) Resize(ctx context.Context, name string, memMB, vcpu int) error {
	if err := m.require(name); err != nil {
		return err
	}
	if err := m.setSize(name, []string{fmt.Sprintf("MICROVM_MEM=%d", memMB), fmt.Sprintf("MICROVM_VCPU=%d", vcpu)}); err != nil {
		return err
	}
	return m.run(ctx, "systemctl", "restart", unit(name))
}

func (m *Manager) ResizeReset(ctx context.Context, name string) error {
	if err := m.require(name); err != nil {
		return err
	}
	if err := m.setSize(name, nil); err != nil {
		return err
	}
	return m.run(ctx, "systemctl", "restart", unit(name))
}

func (m *Manager) Reimage(ctx context.Context, name string) error {
	if err := m.require(name); err != nil {
		return err
	}
	if err := m.run(ctx, "systemctl", "stop", unit(name)); err != nil {
		return err
	}
	for _, img := range []string{"nix-store-overlay.img", "nix-var.img"} {
		if err := os.Remove(filepath.Join(m.dir(name), img)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return m.run(ctx, "systemctl", "start", unit(name))
}

func (m *Manager) Destroy(ctx context.Context, name string) error {
	if err := m.require(name); err != nil {
		return err
	}
	m.run(ctx, "systemctl", "kill", "--signal=SIGKILL", unit(name))
	if err := m.run(ctx, "systemctl", "stop", unit(name)); err != nil {
		return err
	}
	if err := os.RemoveAll(m.dir(name)); err != nil {
		return err
	}
	m.removeGCRoots(name)
	if m.Config.Storage == "zfs" {
		ds := m.zvol(name)
		m.run(ctx, "udevadm", "settle")
		for i := 0; i < 20; i++ {
			if m.run(ctx, "zfs", "destroy", "-r", ds) == nil {
				break
			}
			m.Sleep()
		}
		if m.run(ctx, "zfs", "list", ds) == nil {
			return fmt.Errorf("could not destroy %s", ds)
		}
	}
	return nil
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func (m *Manager) List(ctx context.Context) (string, error) {
	out, err := m.Runner.Run(ctx, "microvm", "-l")
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, line := range strings.Split(ansi.ReplaceAllString(string(out), ""), "\n") {
		if strings.HasPrefix(line, "machine-") {
			b.WriteString(strings.TrimPrefix(line, "machine-") + "\n")
		}
	}
	return b.String(), nil
}

func (m *Manager) GC(ctx context.Context) error {
	dirs, err := filepath.Glob(filepath.Join(m.Config.StateDir, "machine-*"))
	if err != nil {
		return err
	}
	var running []string
	for _, d := range dirs {
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			continue
		}
		u := "microvm@" + filepath.Base(d) + ".service"
		if m.run(ctx, "systemctl", "is-active", "-q", u) == nil {
			running = append(running, u)
		}
	}
	if len(running) > 0 {
		if err := m.run(ctx, "systemctl", append([]string{"stop"}, running...)...); err != nil {
			return err
		}
	}
	if err := m.run(ctx, "nix-collect-garbage"); err != nil {
		return err
	}
	if len(running) > 0 {
		return m.run(ctx, "systemctl", append([]string{"start"}, running...)...)
	}
	return nil
}
