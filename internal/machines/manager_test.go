package machines

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var ctx = context.Background()

func TestCheckNameReserved(t *testing.T) {
	m, _ := newTestManager(t, "image")
	m.LookupUID = func(name string) (int, bool) {
		if name == "messagebus" {
			return 4, true
		}
		if name == "someone" {
			return 1000, true
		}
		return 0, false
	}
	for _, name := range []string{"root", "admin", "guestonly", "messagebus"} {
		err := m.CheckName(name)
		if err == nil || err.Error() != "reserved name '"+name+"'" {
			t.Errorf("CheckName(%q) = %v", name, err)
		}
	}
	for _, name := range []string{"alice", "someone"} {
		if err := m.CheckName(name); err != nil {
			t.Errorf("CheckName(%q) = %v", name, err)
		}
	}
	if err := m.CheckName("Bad"); err == nil || err.Error() != "invalid name 'Bad'" {
		t.Errorf("CheckName(Bad) = %v", err)
	}
}

func TestCreateImageSequence(t *testing.T) {
	m, f := newTestManager(t, "image")
	must(t, m.Create(ctx, "alice", ""))
	f.called(t, "microvm -c machine-alice -t machine")
	f.called(t, "systemctl start microvm@machine-alice.service")
	f.notCalled(t, "zfs")
	d := filepath.Join(m.Config.StateDir, "machine-alice")
	for file, want := range map[string]string{
		"instance/hostname":        "machine-alice\n",
		"instance/user":            "alice\n",
		"instance/authorized_keys": "ssh-ed25519 AAAA flox-machines\n",
		"instance/system":          "/nix/store/abc-nixos-system-machine\n",
	} {
		got, err := os.ReadFile(filepath.Join(d, file))
		must(t, err)
		if string(got) != want {
			t.Errorf("%s = %q, want %q", file, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(d, "owner")); !os.IsNotExist(err) {
		t.Errorf("owner file written without --owner")
	}
}

func TestCreateZFSSequence(t *testing.T) {
	m, f := newTestManager(t, "zfs")
	must(t, m.Create(ctx, "alice", ""))
	f.called(t, "zfs create -s -o com.sun:auto-snapshot=true -V 512M tank/machines/alice")
	f.called(t, "udevadm settle")
	f.called(t, "mkfs.ext4 -q -E root_owner=0:0 -L persist /dev/zvol/tank/machines/alice")
	link, err := os.Readlink(filepath.Join(m.Config.StateDir, "machine-alice", "persist.img"))
	must(t, err)
	if link != "/dev/zvol/tank/machines/alice" {
		t.Errorf("persist.img -> %s", link)
	}
}

func TestCreateOwnerFileMode(t *testing.T) {
	m, _ := newTestManager(t, "image")
	must(t, m.Create(ctx, "bob", "bob@example.com"))
	p := filepath.Join(m.Config.StateDir, "machine-bob", "owner")
	got, err := os.ReadFile(p)
	must(t, err)
	if string(got) != "bob@example.com\n" {
		t.Errorf("owner = %q", got)
	}
	st, err := os.Stat(p)
	must(t, err)
	if st.Mode().Perm() != 0o640 {
		t.Errorf("owner mode = %o", st.Mode().Perm())
	}
}

func TestCreateExisting(t *testing.T) {
	m, _ := newTestManager(t, "image")
	must(t, m.Create(ctx, "alice", ""))
	err := m.Create(ctx, "alice", "")
	if err == nil || err.Error() != "machine 'alice' exists" {
		t.Errorf("second create = %v", err)
	}
}

func TestCreateMissingKey(t *testing.T) {
	m, _ := newTestManager(t, "image")
	must(t, os.Remove(m.Config.KeyPath+".pub"))
	err := m.Create(ctx, "alice", "")
	if err == nil || err.Error() != "admin key "+m.Config.KeyPath+".pub missing" {
		t.Errorf("create = %v", err)
	}
}

func TestCreateCleanupAfterFailedStart(t *testing.T) {
	m, f := newTestManager(t, "zfs")
	f.fail["systemctl start"] = errFake
	if err := m.Create(ctx, "alice", ""); err == nil {
		t.Fatal("create succeeded")
	}
	f.called(t, "zfs destroy -r tank/machines/alice")
	if _, err := os.Lstat(filepath.Join(m.Config.StateDir, "machine-alice")); !os.IsNotExist(err) {
		t.Errorf("machine dir left behind")
	}
	for _, link := range []string{"machine-alice", "booted-machine-alice"} {
		if _, err := os.Lstat(filepath.Join(m.GCRootsDir, link)); !os.IsNotExist(err) {
			t.Errorf("gcroot %s left behind", link)
		}
	}
	delete(f.fail, "systemctl start")
	must(t, m.Create(ctx, "alice", ""))
}

func TestCreateKeepsWhenZvolCleanupFails(t *testing.T) {
	m, f := newTestManager(t, "zfs")
	f.fail["systemctl start"] = errFake
	f.fail["zfs destroy"] = errFake
	err := m.Create(ctx, "alice", "")
	if err == nil || !strings.Contains(err.Error(), "machine destroy alice") {
		t.Fatalf("create = %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.Config.StateDir, "machine-alice")); err != nil {
		t.Errorf("machine dir removed: %v", err)
	}
}

func TestStatusMissing(t *testing.T) {
	m, f := newTestManager(t, "image")
	s, err := m.Status(ctx, "x")
	must(t, err)
	if s.Exists || s.Name != "x" || len(f.calls) != 0 {
		t.Errorf("status = %+v, calls %v", s, f.calls)
	}
}

func TestStatusReachable(t *testing.T) {
	m, f := newTestManager(t, "image")
	must(t, m.Create(ctx, "alice", "alice@example.com"))
	raw, err := os.ReadFile("testdata/tailscale-running.json")
	must(t, err)
	f.out["ssh"] = ""
	f.out["ssh -i "+m.Config.KeyPath+" -o BatchMode=yes -o ConnectTimeout=5 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR root@10.100.0.50 tailscale status --json"] = string(raw)
	s, err := m.Status(ctx, "alice")
	must(t, err)
	if !s.Exists || !s.Running || !s.Reachable || s.Owner != "alice@example.com" {
		t.Errorf("status = %+v", s)
	}
	if s.Tailscale == nil || s.Tailscale.State != "Running" || s.Tailscale.Owner != "alice@example.com" {
		t.Errorf("tailscale = %+v", s.Tailscale)
	}
	f.called(t, "systemctl is-active -q microvm@machine-alice.service")
}

func TestStatusUnreachable(t *testing.T) {
	m, f := newTestManager(t, "image")
	must(t, m.Create(ctx, "alice", ""))
	f.fail["ssh"] = errFake
	s, err := m.Status(ctx, "alice")
	must(t, err)
	if s.Reachable || s.Tailscale != nil {
		t.Errorf("status = %+v", s)
	}
}

func TestLoginRunningIsNoop(t *testing.T) {
	m, f := newTestManager(t, "image")
	must(t, m.Create(ctx, "alice", ""))
	raw, err := os.ReadFile("testdata/tailscale-running.json")
	must(t, err)
	f.out["ssh"] = string(raw)
	must(t, m.Login(ctx, "alice"))
	for _, c := range f.calls {
		if strings.Contains(c, "systemd-run") {
			t.Errorf("login ran %q", c)
		}
	}
}

func TestLoginStarts(t *testing.T) {
	m, f := newTestManager(t, "image")
	must(t, m.Create(ctx, "alice", ""))
	raw, err := os.ReadFile("testdata/tailscale-needslogin.json")
	must(t, err)
	f.out["ssh"] = string(raw)
	must(t, m.Login(ctx, "alice"))
	f.called(t, "ssh -i "+m.Config.KeyPath+" -o BatchMode=yes -o ConnectTimeout=5 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR root@10.100.0.50 systemctl stop machine-tailscale-login.service 2> /dev/null; systemd-run --quiet --collect --unit=machine-tailscale-login tailscale up --ssh")
}

func TestLoginMissing(t *testing.T) {
	m, _ := newTestManager(t, "image")
	if err := m.Login(ctx, "nobody-here"); err == nil || err.Error() != "no machine 'nobody-here'" {
		t.Errorf("login = %v", err)
	}
}

func TestResize(t *testing.T) {
	m, f := newTestManager(t, "image")
	must(t, m.Create(ctx, "alice", ""))
	must(t, m.Resize(ctx, "alice", 2048, 4))
	must(t, m.Resize(ctx, "alice", 2048, 4))
	env, err := os.ReadFile(filepath.Join(m.Config.StateDir, "machine-alice", "instance.env"))
	must(t, err)
	if strings.Count(string(env), "MICROVM_MEM=2048\n") != 1 || strings.Count(string(env), "MICROVM_VCPU=4\n") != 1 {
		t.Errorf("instance.env = %q", env)
	}
	f.called(t, "systemctl restart microvm@machine-alice.service")
	must(t, m.ResizeReset(ctx, "alice"))
	env, err = os.ReadFile(filepath.Join(m.Config.StateDir, "machine-alice", "instance.env"))
	must(t, err)
	if strings.Contains(string(env), "MICROVM_MEM") || strings.Contains(string(env), "MICROVM_VCPU") || !strings.Contains(string(env), "MICROVM_MAC_0") {
		t.Errorf("instance.env after reset = %q", env)
	}
}

func TestReimage(t *testing.T) {
	m, f := newTestManager(t, "image")
	must(t, m.Create(ctx, "alice", ""))
	d := filepath.Join(m.Config.StateDir, "machine-alice")
	must(t, os.WriteFile(filepath.Join(d, "nix-store-overlay.img"), nil, 0o644))
	must(t, os.WriteFile(filepath.Join(d, "nix-var.img"), nil, 0o644))
	must(t, m.Reimage(ctx, "alice"))
	for _, img := range []string{"nix-store-overlay.img", "nix-var.img"} {
		if _, err := os.Stat(filepath.Join(d, img)); !os.IsNotExist(err) {
			t.Errorf("%s kept", img)
		}
	}
	f.called(t, "systemctl stop microvm@machine-alice.service")
}

func TestDestroySequence(t *testing.T) {
	m, f := newTestManager(t, "zfs")
	must(t, m.Create(ctx, "alice", ""))
	f.fail["zfs list"] = errFake
	must(t, m.Destroy(ctx, "alice"))
	f.called(t, "systemctl kill --signal=SIGKILL microvm@machine-alice.service")
	f.called(t, "systemctl stop microvm@machine-alice.service")
	f.called(t, "zfs destroy -r tank/machines/alice")
	if _, err := os.Lstat(filepath.Join(m.Config.StateDir, "machine-alice")); !os.IsNotExist(err) {
		t.Errorf("machine dir kept")
	}
	if _, err := os.Lstat(filepath.Join(m.GCRootsDir, "machine-alice")); !os.IsNotExist(err) {
		t.Errorf("gcroot kept")
	}
}

func TestDestroyZvolStuck(t *testing.T) {
	m, f := newTestManager(t, "zfs")
	must(t, m.Create(ctx, "alice", ""))
	f.fail["zfs destroy"] = errFake
	err := m.Destroy(ctx, "alice")
	if err == nil || err.Error() != "could not destroy tank/machines/alice" {
		t.Errorf("destroy = %v", err)
	}
}

func TestList(t *testing.T) {
	m, f := newTestManager(t, "image")
	f.out["microvm -l"] = "\x1b[1mmachine-alice\x1b[0m: current\nother-vm: current\nmachine-bob: outdated\n"
	got, err := m.List(ctx)
	must(t, err)
	if got != "machine-alice: current\nmachine-bob: outdated\n" {
		t.Errorf("list = %q", got)
	}
}

func TestSSHArgs(t *testing.T) {
	m, _ := newTestManager(t, "image")
	must(t, m.Create(ctx, "alice", ""))
	args, err := m.SSHArgs("alice", []string{"cat", "/etc/hostname"})
	must(t, err)
	want := "ssh -i " + m.Config.KeyPath + " -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR root@10.100.0.50 cat /etc/hostname"
	if strings.Join(args, " ") != want {
		t.Errorf("args = %q", strings.Join(args, " "))
	}
	must(t, os.WriteFile(m.Config.LeasesPath, nil, 0o644))
	if _, err := m.SSHArgs("alice", nil); err == nil || err.Error() != "machine 'alice' has no address yet" {
		t.Errorf("no lease = %v", err)
	}
}

func TestGCStopsAndStartsRunning(t *testing.T) {
	m, f := newTestManager(t, "image")
	must(t, m.Create(ctx, "alice", ""))
	must(t, m.Create(ctx, "bob", ""))
	f.fail["systemctl is-active -q microvm@machine-bob.service"] = errFake
	must(t, m.GC(ctx))
	f.called(t, "systemctl stop microvm@machine-alice.service")
	f.called(t, "nix-collect-garbage")
	f.notCalled(t, "systemctl stop microvm@machine-bob.service")
}
