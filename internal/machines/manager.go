package machines

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var fixedReserved = []string{"admin", "root", "nobody", "sshd", "tailscale", "microvm", "nixbld"}

type Manager struct {
	Config      Config
	Runner      Runner
	GCRootsDir  string
	LookupUID   func(name string) (uid int, ok bool)
	LookupGroup func(name string) (gid int, ok bool)
	Sleep       func()
}

func NewManager(c Config) *Manager {
	return &Manager{
		Config:     c,
		Runner:     ExecRunner{},
		GCRootsDir: "/nix/var/nix/gcroots/microvm",
		LookupUID: func(name string) (int, bool) {
			u, err := user.Lookup(name)
			if err != nil {
				return 0, false
			}
			uid, err := strconv.Atoi(u.Uid)
			return uid, err == nil
		},
		LookupGroup: func(name string) (int, bool) {
			g, err := user.LookupGroup(name)
			if err != nil {
				return 0, false
			}
			gid, err := strconv.Atoi(g.Gid)
			return gid, err == nil
		},
		Sleep: func() { time.Sleep(time.Second) },
	}
}

func (m *Manager) CheckName(name string) error {
	if err := ValidName(name); err != nil {
		return err
	}
	for _, r := range append(append([]string{}, fixedReserved...), m.Config.ReservedNames...) {
		if name == r {
			return fmt.Errorf("reserved name '%s'", name)
		}
	}
	if uid, ok := m.LookupUID(name); ok && uid < 1000 {
		return fmt.Errorf("reserved name '%s'", name)
	}
	return nil
}

func instance(name string) string {
	return "machine-" + name
}

func unit(name string) string {
	return "microvm@" + instance(name) + ".service"
}

func (m *Manager) dir(name string) string {
	return filepath.Join(m.Config.StateDir, instance(name))
}

func (m *Manager) zvol(name string) string {
	return m.Config.ParentDataset + "/" + name
}

func (m *Manager) require(name string) error {
	if err := ValidName(name); err != nil {
		return err
	}
	if st, err := os.Stat(m.dir(name)); err != nil || !st.IsDir() {
		return fmt.Errorf("no machine '%s'", name)
	}
	return nil
}

func (m *Manager) run(ctx context.Context, name string, args ...string) error {
	_, err := m.Runner.Run(ctx, name, args...)
	return err
}

func (m *Manager) removeGCRoots(name string) {
	os.Remove(filepath.Join(m.GCRootsDir, instance(name)))
	os.Remove(filepath.Join(m.GCRootsDir, "booted-"+instance(name)))
}

func (m *Manager) address(name string) string {
	env, err := os.ReadFile(filepath.Join(m.dir(name), "instance.env"))
	if err != nil {
		return ""
	}
	mac := ""
	for _, line := range strings.Split(string(env), "\n") {
		if v, ok := strings.CutPrefix(line, "MICROVM_MAC_0="); ok {
			mac = strings.ToLower(v)
		}
	}
	if mac == "" {
		return ""
	}
	f, err := os.Open(m.Config.LeasesPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	ip := ""
	s := bufio.NewScanner(f)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) >= 3 && strings.ToLower(fields[1]) == mac {
			ip = fields[2]
		}
	}
	return ip
}
