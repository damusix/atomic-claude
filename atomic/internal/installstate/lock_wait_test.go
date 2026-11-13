package installstate

import (
	"bytes"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestAcquireLockNamesTheHolderBeforeWaiting proves a second lifecycle verb
// tells the user who holds the lock instead of hanging silently.
func TestAcquireLockNamesTheHolderBeforeWaiting(t *testing.T) {
	home := newHome(t)
	holder, err := AcquireLock(home, WriterIdentity{OperationID: "op-holder", WriterVersion: "v9.9.9", PID: 4242})
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()

	notice := &lockedBuffer{}
	prev := lockWaitNotice
	lockWaitNotice = notice
	t.Cleanup(func() { lockWaitNotice = prev })

	acquired := make(chan error, 1)
	go func() {
		lock, err := AcquireLock(home, WriterIdentity{OperationID: "op-waiter"})
		if err == nil {
			err = lock.Release()
		}
		acquired <- err
	}()

	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(notice.String(), "op-holder") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	got := notice.String()
	if err := holder.Release(); err != nil {
		t.Fatal(err)
	}
	if err := <-acquired; err != nil {
		t.Fatalf("waiter: %v", err)
	}
	for _, want := range []string{"op-holder", strconv.Itoa(4242), "v9.9.9"} {
		if !strings.Contains(got, want) {
			t.Errorf("wait notice %q does not name %s", got, want)
		}
	}
}

func TestAcquireLockFreeLockPrintsNothing(t *testing.T) {
	home := newHome(t)
	notice := &lockedBuffer{}
	prev := lockWaitNotice
	lockWaitNotice = notice
	t.Cleanup(func() { lockWaitNotice = prev })

	lock, err := AcquireLock(home, WriterIdentity{OperationID: "op-free"})
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	if notice.String() != "" {
		t.Errorf("free lock printed %q", notice.String())
	}
}
