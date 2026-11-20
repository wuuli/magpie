package gui

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/update"
)

// #7347: two starts of one config at the same moment brought the gateway
// up before either owned the single instance. A second start waits for the
// first to let go of the start lock (once it owns the single instance),
// and no longer than its wait.
func TestAppStartWaitsForTheFirst(t *testing.T) {
	dir := t.TempDir()
	endFirst := claimAppStart(dir, time.Second)
	start := time.Now()
	claimAppStart(dir, 200*time.Millisecond)()
	if d := time.Since(start); d < 200*time.Millisecond {
		t.Fatalf("a second start went on after %v, while the first held the start", d)
	}
	done := make(chan time.Time, 1)
	go func() {
		end := claimAppStart(dir, 10*time.Second)
		done <- time.Now()
		end()
	}()
	time.Sleep(100 * time.Millisecond)
	endedAt := time.Now()
	endFirst()
	select {
	case at := <-done:
		if at.Before(endedAt) {
			t.Fatal("the second start went on before the first let go")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the second start still waits after the first let go")
	}
	if _, err := os.Stat(filepath.Join(dir, appStartLock)); err != nil {
		t.Fatalf("the lock isn't in the config dir: %v", err)
	}
}

// #7347: MAGPIE_WV2_TRY went on to the restart to update, so the new
// version began on "start 2 of 3". It is read once and taken out of the
// environment, so only a WebView2 retry passes it on.
func TestWebView2TryTakenOnce(t *testing.T) {
	t.Setenv(webView2TryEnv, "1")
	if n := takeWebView2Try(); n != 1 {
		t.Fatalf("takeWebView2Try = %d, want 1", n)
	}
	if v, ok := os.LookupEnv(webView2TryEnv); ok {
		t.Fatalf("%s=%q left for the next magpie this one starts", webView2TryEnv, v)
	}
	if n := takeWebView2Try(); n != 0 {
		t.Fatalf("a second read = %d, want 0", n)
	}
}

// newMagpie is the staged version's bytes: on Windows a program that
// starts, since the update tries it (suspended) before putting it in
// (update.canStart), and refuses made-up bytes (#6059).
func newMagpie(t *testing.T) []byte {
	t.Helper()
	if runtime.GOOS != "windows" {
		return []byte("new magpie")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// #7347: a restart to update in a process whose WebView2 retry has
// started the next magpie already doesn't start another; it quits for the
// one started, as Settings' Restart does.
func TestRestartAfterWebView2RetryQuits(t *testing.T) {
	sandboxHome(t)
	exe := filepath.Join(t.TempDir(), "magpie")
	os.WriteFile(exe, []byte("running magpie"), 0o755)
	self, _ := os.Stat(exe)
	staged := exe + ".new"
	os.WriteFile(staged, newMagpie(t), 0o755)
	o := updates
	updates = &updater{exe: exe, self: self, staged: staged, state: "ready", latest: &update.Release{Version: "0.1.11"}}
	oldRelaunch, oldHands, oldBundle, oldExe := relaunchBinary, handsOver, bundleOf, executable
	t.Cleanup(func() {
		updates = o
		relaunchBinary, handsOver, bundleOf, executable = oldRelaunch, oldHands, oldBundle, oldExe
	})
	handsOver = func() bool { return false }
	bundleOf = func() string { return "" }
	executable = func() (string, error) { return exe, nil }
	relaunchBinary = func(string, string, bool, string) error { return update.ErrSuccessorStarted }
	if !restartToUpdate(false, false, "") {
		t.Fatal("restart to update stays up, though the next magpie is started")
	}
	if !restartSelfNow(false, false, "") {
		t.Fatal("Settings' Restart stays up, though the next magpie is started")
	}
}
