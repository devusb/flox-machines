package machines

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRunner struct {
	t     *testing.T
	m     *Manager
	calls []string
	out   map[string]string
	fail  map[string]error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	line := strings.TrimSpace(name + " " + strings.Join(args, " "))
	f.calls = append(f.calls, line)
	for prefix, err := range f.fail {
		if strings.HasPrefix(line, prefix) {
			return nil, err
		}
	}
	if name == "microvm" && len(args) > 0 && args[0] == "-c" {
		f.fakeCreate(args[1])
	}
	best, found := "", false
	for prefix := range f.out {
		if strings.HasPrefix(line, prefix) && (!found || len(prefix) > len(best)) {
			best, found = prefix, true
		}
	}
	if found {
		return []byte(f.out[best]), nil
	}
	return nil, nil
}

func (f *fakeRunner) fakeCreate(instance string) {
	d := filepath.Join(f.m.Config.StateDir, instance)
	must(f.t, os.MkdirAll(filepath.Join(d, "instance"), 0o775))
	must(f.t, os.MkdirAll(filepath.Join(d, "current", "share", "microvm"), 0o755))
	must(f.t, os.Symlink("/nix/store/abc-nixos-system-machine", filepath.Join(d, "current", "share", "microvm", "system")))
	must(f.t, os.WriteFile(filepath.Join(d, "instance.env"), []byte("MICROVM_HOSTNAME="+instance+"\nMICROVM_MAC_0=02:AA:BB:CC:DD:EE\n"), 0o644))
	must(f.t, os.Symlink(filepath.Join(d, "current"), filepath.Join(f.m.GCRootsDir, instance)))
	must(f.t, os.Symlink(filepath.Join(d, "booted"), filepath.Join(f.m.GCRootsDir, "booted-"+instance)))
}

func (f *fakeRunner) called(t *testing.T, line string) {
	t.Helper()
	for _, c := range f.calls {
		if c == line {
			return
		}
	}
	t.Errorf("missing call %q in:\n%s", line, strings.Join(f.calls, "\n"))
}

func (f *fakeRunner) notCalled(t *testing.T, prefix string) {
	t.Helper()
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			t.Errorf("unexpected call %q", c)
		}
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

var errFake = errors.New("fake failure")

func newTestManager(t *testing.T, storage string) (*Manager, *fakeRunner) {
	t.Helper()
	root := t.TempDir()
	state := filepath.Join(root, "microvms")
	gcroots := filepath.Join(root, "gcroots")
	must(t, os.MkdirAll(state, 0o775))
	must(t, os.MkdirAll(gcroots, 0o775))
	key := filepath.Join(root, "id_ed25519")
	must(t, os.WriteFile(key, []byte("private"), 0o600))
	must(t, os.WriteFile(key+".pub", []byte("ssh-ed25519 AAAA flox-machines\n"), 0o644))
	leases := filepath.Join(root, "dnsmasq.leases")
	must(t, os.WriteFile(leases, []byte("1 02:aa:bb:cc:dd:ee 10.100.0.50 machine-alice *\n"), 0o644))
	m := &Manager{
		Config: Config{
			StateDir:      state,
			Storage:       storage,
			ParentDataset: "tank/machines",
			PersistSize:   512,
			KeyPath:       key,
			ReservedNames: []string{"guestonly"},
			LeasesPath:    leases,
		},
		GCRootsDir:  gcroots,
		LookupUID:   func(string) (int, bool) { return 0, false },
		LookupGroup: func(string) (int, bool) { return 0, false },
		Sleep:       func() {},
	}
	f := &fakeRunner{t: t, m: m, out: map[string]string{}, fail: map[string]error{}}
	m.Runner = f
	return m, f
}
