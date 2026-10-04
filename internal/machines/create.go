package machines

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func (m *Manager) lock(name string) (func(), error) {
	p := filepath.Join(m.Config.StateDir, ".lock-"+name)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0o660)
	if err != nil {
		return nil, err
	}
	if st, err := f.Stat(); err == nil && st.Mode().Perm() != 0o660 {
		os.Chmod(p, 0o660)
	}
	if gid, ok := m.LookupGroup("kvm"); ok && os.Geteuid() == 0 {
		os.Chown(p, 0, gid)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

func (m *Manager) Create(ctx context.Context, name, owner string) error {
	if err := m.CheckName(name); err != nil {
		return err
	}
	unlock, err := m.lock(name)
	if err != nil {
		return err
	}
	defer unlock()
	d := m.dir(name)
	if _, err := os.Lstat(d); err == nil {
		return fmt.Errorf("machine '%s' exists", name)
	}
	pub, err := os.ReadFile(m.Config.KeyPath + ".pub")
	if err != nil {
		return fmt.Errorf("admin key %s.pub missing", m.Config.KeyPath)
	}
	if err := m.run(ctx, "microvm", "-c", instance(name), "-t", "machine"); err != nil {
		return err
	}
	zvolCreated := false
	if err := m.populate(ctx, name, owner, pub, &zvolCreated); err != nil {
		return m.cleanup(ctx, name, zvolCreated, err)
	}
	return nil
}

func (m *Manager) populate(ctx context.Context, name, owner string, pub []byte, zvolCreated *bool) error {
	d := m.dir(name)
	system, err := os.Readlink(filepath.Join(d, "current", "share", "microvm", "system"))
	if err != nil {
		return err
	}
	for file, content := range map[string]string{
		"hostname":        instance(name) + "\n",
		"user":            name + "\n",
		"authorized_keys": string(pub),
		"system":          system + "\n",
	} {
		if err := os.WriteFile(filepath.Join(d, "instance", file), []byte(content), 0o644); err != nil {
			return err
		}
	}
	root := os.Geteuid() == 0
	if m.Config.Storage == "zfs" {
		ds := m.zvol(name)
		if err := m.run(ctx, "zfs", "create", "-s", "-o", "com.sun:auto-snapshot=true", "-V", fmt.Sprintf("%dM", m.Config.PersistSize), ds); err != nil {
			return err
		}
		*zvolCreated = true
		if err := m.run(ctx, "udevadm", "settle"); err != nil {
			return err
		}
		dev := "/dev/zvol/" + ds
		if err := m.run(ctx, "mkfs.ext4", "-q", "-E", "root_owner=0:0", "-L", "persist", dev); err != nil {
			return err
		}
		if root {
			if err := m.run(ctx, "chown", "-L", "microvm:kvm", dev); err != nil {
				return err
			}
		}
		if err := os.Symlink(dev, filepath.Join(d, "persist.img")); err != nil {
			return err
		}
	}
	if root {
		if err := m.run(ctx, "chown", "-R", "microvm:kvm", d); err != nil {
			return err
		}
	}
	if owner != "" {
		if err := m.writeOwner(d, owner, root); err != nil {
			return err
		}
	}
	return m.run(ctx, "systemctl", "start", unit(name))
}

func (m *Manager) writeOwner(d, owner string, root bool) error {
	p := filepath.Join(d, "owner")
	if err := os.WriteFile(p, []byte(owner+"\n"), 0o640); err != nil {
		return err
	}
	if err := os.Chmod(p, 0o640); err != nil {
		return err
	}
	uid := -1
	if root {
		uid = 0
	}
	if gid, ok := m.LookupGroup("kvm"); ok {
		return os.Chown(p, uid, gid)
	}
	return nil
}

func (m *Manager) cleanup(ctx context.Context, name string, zvolCreated bool, cause error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	if zvolCreated {
		if err := m.run(ctx, "zfs", "destroy", "-r", m.zvol(name)); err != nil {
			return fmt.Errorf("%w; %s was left in place, run 'machine destroy %s'", cause, strings.TrimSpace(m.zvol(name)), name)
		}
	}
	m.removeGCRoots(name)
	if err := os.RemoveAll(m.dir(name)); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}
