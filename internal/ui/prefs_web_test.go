package ui

import (
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

// webHooks records what the Web server tab asks of the application.
type webHooks struct {
	toggles, changed int
	running          bool
	url              string
}

func (w *webHooks) apply(h *hookLog, d *PreferencesDialog) {
	d.hooks.WebToggle = func() { w.toggles++ }
	d.hooks.WebState = func() (bool, string) { return w.running, w.url }
	d.hooks.WebChanged = func() { w.changed++ }
	d.hooks.Changed = func() { h.changed++ }
}

func TestWebTabShowsTheStoredSettings(t *testing.T) {
	b := newFakeBackend()
	b.web = WebSettings{Port: 4000, HasPassword: true}
	withPrefs(t, b, func(d *PreferencesDialog, h *hookLog) {
		if d.WebPortValue() != 4000 {
			t.Errorf("port = %d, want 4000", d.WebPortValue())
		}
		if !strings.Contains(d.web.access.Text(), "A password is set") || !d.web.clearPass.IsEnabled() || d.web.setPass.Text() != "Change password..." {
			t.Errorf("access = %q, clear enabled %v, set %q", d.web.access.Text(), d.web.clearPass.IsEnabled(), d.web.setPass.Text())
		}
		if len(b.webPorts) != 0 || len(b.webPasswords) != 0 {
			t.Error("filling the dialog must not store anything")
		}
	})
}

func TestWebTabDefaults(t *testing.T) {
	withPrefs(t, newFakeBackend(), func(d *PreferencesDialog, _ *hookLog) {
		if d.WebPortValue() != 3333 {
			t.Errorf("default port = %d, want 3333", d.WebPortValue())
		}
		if !strings.Contains(d.web.access.Text(), "only this computer") || d.web.clearPass.IsEnabled() || d.web.setPass.Text() != "Set password..." {
			t.Errorf("access = %q", d.web.access.Text())
		}
		if d.web.status.Text() != "The web server is stopped." || d.web.toggleBtn.Text() != "Start web server" {
			t.Errorf("status %q, button %q", d.web.status.Text(), d.web.toggleBtn.Text())
		}
	})
}

func TestWebTabStatusFollowsTheServer(t *testing.T) {
	withPrefs(t, newFakeBackend(), func(d *PreferencesDialog, h *hookLog) {
		w := &webHooks{url: "http://localhost:3333/"}
		w.apply(h, d)
		d.web.toggleBtn.Click()
		if w.toggles != 1 {
			t.Fatalf("toggles = %d", w.toggles)
		}
		w.running = true
		d.SetWebState()
		if !strings.Contains(d.web.status.Text(), "running at http://localhost:3333/") || d.web.toggleBtn.Text() != "Stop web server" {
			t.Errorf("status %q, button %q", d.web.status.Text(), d.web.toggleBtn.Text())
		}
		w.running = false
		d.SetWebState()
		if d.web.toggleBtn.Text() != "Start web server" {
			t.Errorf("button %q", d.web.toggleBtn.Text())
		}
	})
}

func TestWebPortChangeIsStoredAndReported(t *testing.T) {
	b := newFakeBackend()
	withPrefs(t, b, func(d *PreferencesDialog, h *hookLog) {
		w := &webHooks{}
		w.apply(h, d)
		d.web.port.SetValue(8080)
		if len(b.webPorts) != 1 || b.webPorts[0] != 8080 || w.changed != 1 {
			t.Errorf("stored %v, reported %d times", b.webPorts, w.changed)
		}
		b.webErr = errors.New("DatabaseQueryFailed", "disk full")
		d.web.port.SetValue(9090)
		if d.banner.IsHidden() || !strings.Contains(d.banner.Text(), "disk full") || len(h.failed) != 1 {
			t.Errorf("a failure shows in the banner: %q hidden=%v", d.banner.Text(), d.banner.IsHidden())
		}
		if w.changed != 1 {
			t.Error("a failed save is not reported as a change")
		}
	})
}

func TestWebPasswordSetAndRemove(t *testing.T) {
	b := newFakeBackend()
	withPrefs(t, b, func(d *PreferencesDialog, h *hookLog) {
		w := &webHooks{}
		w.apply(h, d)

		// Cancelled: nothing happens.
		d.askPassword = func() (string, string, bool) { return "", "", false }
		d.web.setPass.Click()
		if len(b.webPasswords) != 0 {
			t.Error("a cancelled question stores nothing")
		}

		// The two entries differ.
		d.askPassword = func() (string, string, bool) { return "one-password", "another-one", true }
		d.web.setPass.Click()
		if len(b.webPasswords) != 0 || !strings.Contains(d.banner.Text(), "not the same") {
			t.Errorf("stored %v, banner %q", b.webPasswords, d.banner.Text())
		}

		// A good password.
		d.askPassword = func() (string, string, bool) { return "correct horse", "correct horse", true }
		d.web.setPass.Click()
		if len(b.webPasswords) != 1 || b.webPasswords[0] != "correct horse" || w.changed != 1 {
			t.Errorf("stored %v, reported %d", b.webPasswords, w.changed)
		}
		if !d.banner.IsHidden() || !strings.Contains(d.web.access.Text(), "A password is set") || !d.web.clearPass.IsEnabled() {
			t.Errorf("after setting: banner %q, access %q", d.banner.Text(), d.web.access.Text())
		}

		// The backend refuses a weak one: its message is shown.
		b.webErr = errors.New("WebPasswordInvalid", "The password must have at least 8 characters.")
		d.askPassword = func() (string, string, bool) { return "x", "x", true }
		d.web.setPass.Click()
		if !strings.Contains(d.banner.Text(), "at least 8 characters") {
			t.Errorf("banner = %q", d.banner.Text())
		}
		b.webErr = nil

		// Remove, after asking.
		d.confirm = func(string) bool { return false }
		d.web.clearPass.Click()
		if len(b.webPasswords) != 1 {
			t.Error("declining keeps the password")
		}
		d.confirm = func(string) bool { return true }
		d.web.clearPass.Click()
		if len(b.webPasswords) != 2 || b.webPasswords[1] != "" || w.changed != 2 {
			t.Errorf("stored %q, reported %d", b.webPasswords, w.changed)
		}
		if d.web.clearPass.IsEnabled() || !strings.Contains(d.web.access.Text(), "only this computer") {
			t.Errorf("after removing: %q", d.web.access.Text())
		}
	})
}

func TestWebTabLoadFailureShowsABannerAndDefaults(t *testing.T) {
	b := newFakeBackend()
	b.webErr = errors.New("DatabaseQueryFailed", "no database")
	withPrefs(t, b, func(d *PreferencesDialog, h *hookLog) {
		if d.WebPortValue() != 3333 || d.banner.IsHidden() {
			t.Errorf("port %d, banner hidden %v", d.WebPortValue(), d.banner.IsHidden())
		}
	})
}
