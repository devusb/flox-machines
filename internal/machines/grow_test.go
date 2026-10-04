package machines

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGrowStoreImage(t *testing.T) {
	m, f := newTestManager(t, "image")
	must(t, m.Create(ctx, "alice", ""))
	img := filepath.Join(m.Config.StateDir, "machine-alice", "nix-store-overlay.img")
	must(t, os.WriteFile(img, nil, 0o644))
	must(t, os.Truncate(img, 2048<<20))
	must(t, m.Grow(ctx, "alice", "store", 3072))
	st, err := os.Stat(img)
	must(t, err)
	if st.Size() != 3072<<20 {
		t.Errorf("size = %d", st.Size())
	}
	f.called(t, "systemctl stop microvm@machine-alice.service")
	f.called(t, "e2fsck -f -p "+img)
	f.called(t, "resize2fs "+img)
	f.called(t, "systemctl start microvm@machine-alice.service")
}

func TestGrowPersistZvol(t *testing.T) {
	m, f := newTestManager(t, "zfs")
	must(t, m.Create(ctx, "alice", ""))
	f.out["zfs get -Hp -o value volsize tank/machines/alice"] = "536870912\n"
	must(t, m.Grow(ctx, "alice", "persist", 1024))
	f.called(t, "zfs set volsize=1024M tank/machines/alice")
	f.called(t, "udevadm settle")
	f.called(t, "e2fsck -f -p /dev/zvol/tank/machines/alice")
	f.called(t, "resize2fs /dev/zvol/tank/machines/alice")
}

func TestGrowPersistImage(t *testing.T) {
	m, f := newTestManager(t, "image")
	must(t, m.Create(ctx, "alice", ""))
	img := filepath.Join(m.Config.StateDir, "machine-alice", "persist.img")
	must(t, os.WriteFile(img, nil, 0o644))
	must(t, os.Truncate(img, 512<<20))
	must(t, m.Grow(ctx, "alice", "persist", 1024))
	f.notCalled(t, "zfs")
	f.called(t, "resize2fs "+img)
}

func TestGrowRefusesShrink(t *testing.T) {
	m, f := newTestManager(t, "zfs")
	must(t, m.Create(ctx, "alice", ""))
	f.out["zfs get -Hp -o value volsize tank/machines/alice"] = "536870912\n"
	for _, size := range []int{256, 512} {
		err := m.Grow(ctx, "alice", "persist", size)
		if err == nil || err.Error() != "persist is already 512 MB" {
			t.Errorf("Grow to %d = %v", size, err)
		}
	}
	f.notCalled(t, "systemctl stop")
}

func TestGrowAcceptsCorrectedFsck(t *testing.T) {
	m, f := newTestManager(t, "zfs")
	must(t, m.Create(ctx, "alice", ""))
	f.out["zfs get -Hp -o value volsize tank/machines/alice"] = "536870912\n"
	f.fail["e2fsck"] = &ExitError{Code: 1, Msg: "corrected"}
	must(t, m.Grow(ctx, "alice", "persist", 1024))
	f.called(t, "resize2fs /dev/zvol/tank/machines/alice")
}

func TestGrowFailedFsck(t *testing.T) {
	m, f := newTestManager(t, "zfs")
	must(t, m.Create(ctx, "alice", ""))
	f.out["zfs get -Hp -o value volsize tank/machines/alice"] = "536870912\n"
	f.fail["e2fsck"] = &ExitError{Code: 4, Msg: "uncorrected"}
	if err := m.Grow(ctx, "alice", "persist", 1024); err == nil {
		t.Fatal("grow succeeded")
	}
	f.notCalled(t, "resize2fs")
}

func TestGrowStoppedStaysStopped(t *testing.T) {
	m, f := newTestManager(t, "zfs")
	must(t, m.Create(ctx, "alice", ""))
	f.out["zfs get -Hp -o value volsize tank/machines/alice"] = "536870912\n"
	f.fail["systemctl is-active"] = &ExitError{Code: 3}
	f.calls = nil
	must(t, m.Grow(ctx, "alice", "persist", 1024))
	f.notCalled(t, "systemctl start")
}

func TestGrowBadVolume(t *testing.T) {
	m, _ := newTestManager(t, "image")
	must(t, m.Create(ctx, "alice", ""))
	if err := m.Grow(ctx, "alice", "home", 1024); err == nil || err.Error() != "usage: machine grow <name> persist|store <MB>" {
		t.Errorf("grow = %v", err)
	}
	if err := m.Grow(ctx, "alice", "store", 1024); err == nil || err.Error() != "machine 'alice' has no store volume yet" {
		t.Errorf("grow without image = %v", err)
	}
}
