package update

import (
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// freshSuccessor has the test start as a process that has started no
// successor yet, and leaves it so.
func freshSuccessor(t *testing.T) {
	t.Helper()
	successor.mu.Lock()
	successor.started = false
	successor.mu.Unlock()
	t.Cleanup(func() {
		successor.mu.Lock()
		successor.started = false
		successor.mu.Unlock()
	})
}

// #7347: a Windows magpie stuck waiting for WebView2 restarted to update
// (RelaunchBinary) and then, its wait timed out, started again for the
// retry (Respawn). Two new magpies waited for it to exit, raced for the
// gateway's port and the single instance, and both died. A process starts
// one successor, whichever asks first, in either order.
func TestOneSuccessorPerProcess(t *testing.T) {
	exe, _ := relaunchFiles(t)
	for _, updateFirst := range []bool{true, false} {
		var launches atomic.Int32
		stubLaunch(t, func(string, []string, []string) (<-chan struct{}, func(), error) {
			launches.Add(1)
			return make(chan struct{}), func() {}, nil
		})
		restart := func() error { return RelaunchBinary(exe, "", false, "") }
		retry := func() error { return Respawn("MAGPIE_WV2_TRY=2") }
		first, second := restart, retry
		if !updateFirst {
			first, second = retry, restart
		}
		if err := first(); err != nil {
			t.Fatalf("updateFirst=%v: first start: %v", updateFirst, err)
		}
		if err := second(); !errors.Is(err, ErrSuccessorStarted) {
			t.Fatalf("updateFirst=%v: second start = %v, want ErrSuccessorStarted", updateFirst, err)
		}
		if n := launches.Load(); n != 1 {
			t.Fatalf("updateFirst=%v: %d magpies started, want 1", updateFirst, n)
		}
	}
}

// A restart whose new version was refused leaves this one running
// (#894), so a WebView2 retry after it still starts one: the refusal frees
// the claim. A retry asked while the restart is still waiting for its new
// version's word waits for that outcome instead of starting a second.
func TestSuccessorClaimWaitsForTheOutcome(t *testing.T) {
	for _, refused := range []bool{true, false} {
		exe, old := relaunchFiles(t)
		var launches atomic.Int32
		release := make(chan struct{})
		stubLaunch(t, func(_ string, _, env []string) (<-chan struct{}, func(), error) {
			if launches.Add(1) > 1 {
				return make(chan struct{}), func() {}, nil // the retry's start
			}
			<-release
			if refused {
				return nil, nil, &os.PathError{Op: "fork/exec", Path: exe, Err: errIntegrityPolicy}
			}
			for _, kv := range env {
				if p, ok := strings.CutPrefix(kv, startedEnv+"="); ok {
					os.WriteFile(p, []byte("1"), 0o644)
				}
			}
			return make(chan struct{}), func() {}, nil
		})
		restarted := make(chan error, 1)
		go func() { restarted <- RelaunchBinary(exe, old, false, "") }()
		for launches.Load() == 0 {
			time.Sleep(time.Millisecond)
		}
		retried := make(chan error, 1)
		go func() { retried <- Respawn("MAGPIE_WV2_TRY=1") }()
		select {
		case err := <-retried:
			t.Fatalf("refused=%v: the retry answered %v before the restart's outcome", refused, err)
		case <-time.After(50 * time.Millisecond):
		}
		close(release)
		rerr, err := <-restarted, <-retried
		if refused {
			if !errors.As(rerr, new(*BlockedError)) || err != nil || launches.Load() != 2 {
				t.Fatalf("refused restart: restart %v, retry %v, %d starts; want the retry to start one", rerr, err, launches.Load())
			}
			continue
		}
		if rerr != nil || !errors.Is(err, ErrSuccessorStarted) || launches.Load() != 1 {
			t.Fatalf("restart %v, retry %v, %d starts; want one start", rerr, err, launches.Load())
		}
	}
}
