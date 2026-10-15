// Package uitest runs Go tests that need Qt. Qt wants one thread for all GUI
// calls, but Go runs each test in its own goroutine, so Main runs the Qt event
// loop on the main thread and the tests run beside it, calling OnUI to reach
// the GUI. Tests use QT_QPA_PLATFORM=offscreen, so no display is needed.
package uitest

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"
)

// Main is for a package's TestMain:
//
//	func TestMain(m *testing.M) { uitest.Main(m) }
//
// If the offscreen platform plugin is missing, Qt cannot start; Main then
// prints that the tests were skipped and exits successfully.
func Main(m *testing.M) {
	runtime.LockOSThread()
	if os.Getenv("QT_QPA_PLATFORM") == "" {
		if err := os.Setenv("QT_QPA_PLATFORM", "offscreen"); err != nil {
			fmt.Fprintln(os.Stderr, "uitest: cannot set QT_QPA_PLATFORM:", err)
			os.Exit(1)
		}
	}
	if os.Getenv("QT_QPA_PLATFORM") == "offscreen" && !offscreenAvailable() {
		fmt.Println("SKIP: the Qt offscreen platform plugin was not found; Qt tests were not run")
		os.Exit(0)
	}

	qt.NewQApplication([]string{os.Args[0]})
	// Tests open and close many windows; the event loop must outlive them.
	qt.QGuiApplication_SetQuitOnLastWindowClosed(false)
	result := make(chan int, 1)
	go func() {
		code := m.Run()
		mainthread.Start(func() { qt.QCoreApplication_Quit() })
		result <- code
	}()
	qt.QApplication_Exec()
	os.Exit(<-result)
}

func offscreenAvailable() bool {
	plugins := qt.QLibraryInfo_Path(qt.QLibraryInfo__PluginsPath)
	matches, err := filepath.Glob(filepath.Join(plugins, "platforms", "*qoffscreen*"))
	return err == nil && len(matches) > 0
}

// OnUI runs f on the Qt main thread and waits for it. Inside f use t.Errorf,
// never t.Fatal: Fatal would stop the wrong goroutine.
func OnUI(f func()) { mainthread.Wait(f) }
