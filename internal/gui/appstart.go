package gui

import (
	"log"
	"path/filepath"
	"time"

	"github.com/yetone/magpie/internal/filelock"
)

// Two starts of one config at the same moment (#7347: a restart to update
// and a WebView2 retry, both waiting on the same magpie to exit) each
// brought the gateway up before Wails took its single-instance mutex in
// application.New. The one that bound the port could then lose the mutex
// and be os.Exit'ed by Wails as the second instance, and the gateway went
// with it. A start holds appStartLock from before the gateway until it
// owns the single instance, so a second start finds the first's instance
// and hands its arguments over to it.

// appStartLock is the lock file, in the config dir the single instance's
// id is made from.
const appStartLock = "app-start.lock"

// appStartWait is how long a start waits for another to own the single
// instance: past the WebView2 folder's 30s and a retry's backoff. After
// it, it goes on as before there was a lock.
const appStartWait = 2 * time.Minute

// claimAppStart takes the start lock in dir, waiting up to wait for
// another start to let go of it; the func lets go of it.
func claimAppStart(dir string, wait time.Duration) func() {
	start := time.Now()
	unlock, held := filelock.Lock(filepath.Join(dir, appStartLock), wait)
	if held {
		if d := time.Since(start); d > time.Second {
			log.Printf("magpie: waited %s for another start of magpie to take the single instance", d.Round(100*time.Millisecond))
		}
	}
	return unlock
}
