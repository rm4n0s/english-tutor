package app

import (
	"context"

	"github.com/rm4n0s/english-tutor/internal/ui"
	"github.com/rm4n0s/english-tutor/internal/web"
)

// toggleWeb starts the web server, or stops it if it runs (the Web button of the
// window and the tray item).
func (a *App) toggleWeb() {
	if a.web.Running() {
		a.StopWeb()
		return
	}
	a.StartWeb()
}

// StartWeb starts the web server with the stored port and password. If it
// cannot start (the port is in use, for one), the reason is shown in the window
// and, when the window is hidden in the tray, next to the icon. It reports
// whether the server runs.
func (a *App) StartWeb() bool {
	if a.web.Running() {
		return true
	}
	port, err := a.store.WebPort(a.ctx)
	if err != nil {
		a.showError("reading web server port", err)
		return false
	}
	hash, err := a.store.WebPasswordHash(a.ctx)
	if err != nil {
		a.showError("reading web server password", err)
		return false
	}
	if err := a.web.Start(a.ctx, web.Options{Port: port, PasswordHash: hash}); err != nil {
		a.showError("starting the web server", err)
		a.notify("Web server", userMessage(err))
		a.refreshWeb()
		return false
	}
	a.refreshWeb()
	a.notify("Web server", "The web server runs at "+a.web.URL())
	return true
}

// StopWeb stops the web server. The pages that are open in browsers end with it.
func (a *App) StopWeb() {
	a.web.Stop()
	a.refreshWeb()
}

// refreshWeb shows whether the server runs in the window, the tray and the open
// Preferences dialog.
func (a *App) refreshWeb() {
	running, url := a.web.Running(), a.web.URL()
	a.win.SetWebRunning(running, url)
	if a.tray != nil {
		a.tray.SetWebRunning(running)
	}
	if a.prefsDlg != nil {
		a.prefsDlg.SetWebState()
	}
}

// notify shows a message next to the tray icon, if the application has one.
func (a *App) notify(title, text string) {
	if a.tray != nil {
		a.tray.Notify(title, text)
	}
}

// openWebPage opens the page of the web server in the browser.
func (a *App) openWebPage() {
	if url := a.web.URL(); url != "" {
		a.openBrowser(url)
	}
}

// webState is what the Preferences dialog shows about the server.
func (a *App) webState() (running bool, url string) { return a.web.Running(), a.web.URL() }

// onWebSettingsChanged applies a new port or password to a server that runs, by
// starting it again. If it cannot start on the new port it stays stopped, and
// the reason is shown.
func (a *App) onWebSettingsChanged() {
	if !a.web.Running() {
		return
	}
	a.web.Stop()
	a.StartWeb()
}

// WebSettings, SetWebPort and SetWebPassword are the Web server tab of the
// Preferences dialog.
func (p preferences) WebSettings(ctx context.Context) (ui.WebSettings, error) {
	port, err := p.store.WebPort(ctx)
	if err != nil {
		return ui.WebSettings{}, err
	}
	hash, err := p.store.WebPasswordHash(ctx)
	if err != nil {
		return ui.WebSettings{}, err
	}
	return ui.WebSettings{Port: port, HasPassword: hash != ""}, nil
}

func (p preferences) SetWebPort(ctx context.Context, port int) error {
	return p.store.SetWebPort(ctx, port)
}

// SetWebPassword stores the hash of the password. An empty password removes it.
func (p preferences) SetWebPassword(ctx context.Context, password string) error {
	if password == "" {
		return p.store.SetWebPasswordHash(ctx, "")
	}
	hash, err := web.HashPassword(password)
	if err != nil {
		return err
	}
	return p.store.SetWebPasswordHash(ctx, hash)
}
