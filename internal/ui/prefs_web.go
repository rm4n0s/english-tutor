package ui

import (
	qt "github.com/mappu/miqt/qt6"

	"github.com/rm4n0s/english-tutor/internal/store"
)

// WebSettings holds the settings of the Web server tab (section 4.7). The
// password itself is never shown or kept: only whether there is one.
type WebSettings struct {
	Port        int
	HasPassword bool
}

// webTab is the widgets of the Web server tab.
type webTab struct {
	status    *qt.QLabel
	toggleBtn *qt.QPushButton
	port      *qt.QSpinBox
	access    *qt.QLabel
	setPass   *qt.QPushButton
	clearPass *qt.QPushButton
	loading   bool // true while the widgets are filled from the settings
	settings  WebSettings
}

const webSecurityNote = "The page is served over plain HTTP, so a password sent from another computer can be read on the network. " +
	"Use it only on a network you trust."

func (d *PreferencesDialog) buildWebTab() *qt.QWidget {
	tab := qt.NewQWidget2()
	layout := qt.NewQVBoxLayout2()
	tab.SetLayout(layout.QLayout)

	intro := qt.NewQLabel3("The web server shows this window in a web browser, with the same functions. " +
		"Open and Save become Open file and Download.")
	intro.SetWordWrap(true)
	layout.AddWidget(intro.QWidget)

	row := qt.NewQHBoxLayout2()
	d.web.status = qt.NewQLabel3("")
	d.web.status.SetTextInteractionFlags(qt.TextSelectableByMouse)
	d.web.toggleBtn = qt.NewQPushButton3(webStartText)
	row.AddWidget2(d.web.status.QWidget, 1)
	row.AddWidget(d.web.toggleBtn.QWidget)
	layout.AddLayout(row.QLayout)

	form := qt.NewQFormLayout2()
	d.web.port = qt.NewQSpinBox2()
	d.web.port.SetRange(1, 65535)
	d.web.port.SetKeyboardTracking(false) // a new port counts when typing ends, not at every digit
	d.web.port.SetToolTip("The default is 3333. A running server restarts on the new port.")
	form.AddRow3("Port", d.web.port.QWidget)
	layout.AddLayout(form.QLayout)

	d.web.access = qt.NewQLabel3("")
	d.web.access.SetWordWrap(true)
	layout.AddWidget(d.web.access.QWidget)

	buttons := qt.NewQHBoxLayout2()
	d.web.setPass = qt.NewQPushButton3("Set password...")
	d.web.clearPass = qt.NewQPushButton3("Remove password")
	buttons.AddWidget(d.web.setPass.QWidget)
	buttons.AddWidget(d.web.clearPass.QWidget)
	buttons.AddStretch()
	layout.AddLayout(buttons.QLayout)

	note := qt.NewQLabel3(webSecurityNote)
	note.SetWordWrap(true)
	note.SetStyleSheet("font-style: italic;")
	layout.AddWidget(note.QWidget)
	layout.AddStretch()

	d.web.toggleBtn.OnClicked(func() { call(d.hooks.WebToggle) })
	d.web.port.OnValueChanged(func(int) { d.webPortChanged() })
	d.web.setPass.OnClicked(d.setWebPassword)
	d.web.clearPass.OnClicked(d.clearWebPassword)
	return tab
}

// webStartText and webStopText are the texts of the button that starts and
// stops the server, here and in the main window.
const (
	webStartText = "Start web server"
	webStopText  = "Stop web server"
)

func (d *PreferencesDialog) loadWeb() {
	s, err := d.backend.WebSettings(d.ctx)
	if err != nil {
		d.fail(err)
		s = WebSettings{Port: store.DefaultWebPort}
	}
	d.web.loading = true
	d.web.settings = s
	d.web.port.SetValue(s.Port)
	d.web.loading = false
	d.updateWebAccess()
	d.SetWebState()
}

// SetWebState refreshes the status line and the button from the server. The
// application calls it when the server starts or stops while the dialog is open.
func (d *PreferencesDialog) SetWebState() {
	running, url := false, ""
	if d.hooks.WebState != nil {
		running, url = d.hooks.WebState()
	}
	if running {
		d.web.status.SetText("The web server is running at " + url)
		d.web.toggleBtn.SetText(webStopText)
		return
	}
	d.web.status.SetText("The web server is stopped.")
	d.web.toggleBtn.SetText(webStartText)
}

func (d *PreferencesDialog) updateWebAccess() {
	if d.web.settings.HasPassword {
		d.web.access.SetText("A password is set. This computer is let in as it is; other computers must sign in with the password.")
	} else {
		d.web.access.SetText("No password is set, so only this computer can open the page (localhost). " +
			"Set a password to let other computers in.")
	}
	d.web.clearPass.SetEnabled(d.web.settings.HasPassword)
	d.web.setPass.SetText(map[bool]string{true: "Change password...", false: "Set password..."}[d.web.settings.HasPassword])
}

func (d *PreferencesDialog) webPortChanged() {
	if d.web.loading {
		return
	}
	port := d.web.port.Value()
	if err := d.backend.SetWebPort(d.ctx, port); err != nil {
		d.fail(err)
		return
	}
	d.web.settings.Port = port
	d.clearBanner()
	call(d.hooks.WebChanged)
	d.SetWebState()
}

// askPasswordTwice asks for a password and its repetition.
func (d *PreferencesDialog) askPasswordTwice() (string, string, bool) {
	ok := false
	first := qt.QInputDialog_GetText4(d.dlg.QWidget, "Web server password", "New password:", qt.QLineEdit__Password, "", &ok)
	if !ok {
		return "", "", false
	}
	second := qt.QInputDialog_GetText4(d.dlg.QWidget, "Web server password", "Type it again:", qt.QLineEdit__Password, "", &ok)
	return first, second, ok
}

func (d *PreferencesDialog) setWebPassword() {
	first, second, ok := d.askPassword()
	if !ok {
		return
	}
	if first != second {
		d.showMessage("The two passwords are not the same.")
		return
	}
	if err := d.backend.SetWebPassword(d.ctx, first); err != nil {
		d.fail(err)
		return
	}
	d.web.settings.HasPassword = true
	d.updateWebAccess()
	d.clearBanner()
	call(d.hooks.WebChanged)
	d.SetWebState()
}

func (d *PreferencesDialog) clearWebPassword() {
	if !d.confirm("Remove the password? Then only this computer can open the web page.") {
		return
	}
	if err := d.backend.SetWebPassword(d.ctx, ""); err != nil {
		d.fail(err)
		return
	}
	d.web.settings.HasPassword = false
	d.updateWebAccess()
	d.clearBanner()
	call(d.hooks.WebChanged)
	d.SetWebState()
}

// showMessage shows a message of the dialog's own in the banner.
func (d *PreferencesDialog) showMessage(text string) {
	d.banner.SetText(text)
	d.banner.Show()
}

// WebPortValue returns the port shown, for tests.
func (d *PreferencesDialog) WebPortValue() int { return d.web.port.Value() }
