package realmshare

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRestorationIdentityRejectsMissingBirthTime(t *testing.T) {
	file, err := os.Open("/proc/self/status")
	if err != nil {
		t.Skipf("procfs fixture unavailable: %v", err)
	}
	defer file.Close()

	var stat unix.Statx_t
	if err := unix.Statx(int(file.Fd()), "", unix.AT_EMPTY_PATH, unix.STATX_INO|unix.STATX_BTIME, &stat); err != nil {
		t.Skipf("statx unavailable for procfs fixture: %v", err)
	}
	if stat.Mask&unix.STATX_INO == 0 || stat.Ino == 0 || stat.Mask&unix.STATX_BTIME != 0 {
		t.Skipf("procfs fixture does not provide an inode without birth time: mask %#x, inode %d", stat.Mask, stat.Ino)
	}

	identity, err := restorationIdentity(file)
	if err == nil || identity != "" {
		t.Fatalf("restorationIdentity without birth time = (%q, %v), want an empty identity and an error", identity, err)
	}
}
