package ui

import (
	qt "github.com/mappu/miqt/qt6"
)

// TrayActions are what the tray menu does. Each one runs on the UI thread; a nil
// one does nothing.
type TrayActions struct {
	// Open shows the main window, Quit ends the application.
	Open, Quit func()
	// ToggleWeb starts or stops the web server, OpenWeb opens its page in the
	// browser (section 4.7).
	ToggleWeb, OpenWeb func()
}

// Tray is the system tray icon with its menu: Open shows the main window, Start
// (or Stop) web server and Open web page deal with the web server, and Close
// quits the application.
type Tray struct {
	icon       *qt.QSystemTrayIcon
	menu       *qt.QMenu
	openAct    *qt.QAction
	webAct     *qt.QAction
	openWebAct *qt.QAction
	quitAct    *qt.QAction
	open       func()
}

// TrayAvailable reports whether the desktop has a system tray. Without one the
// application must not hide its window, because nothing could bring it back.
func TrayAvailable() bool { return qt.QSystemTrayIcon_IsSystemTrayAvailable() }

// Menu texts of the web server items.
const (
	trayStartWeb = "Start web server"
	trayStopWeb  = "Stop web server"
	trayOpenWeb  = "Open web page"
)

// NewTray builds the tray icon. The icon is not visible until Show is called.
func NewTray(a TrayActions) *Tray {
	t := &Tray{open: a.Open}
	fallback := qt.QApplication_Style().StandardIcon(qt.QStyle__SP_FileIcon, nil, nil)
	t.icon = qt.NewQSystemTrayIcon2(qt.QIcon_FromTheme2("accessories-text-editor", fallback))
	t.icon.SetToolTip("English Tutor")
	t.menu = qt.NewQMenu2()
	t.openAct = t.menu.AddActionWithText("Open")
	t.webAct = t.menu.AddActionWithText(trayStartWeb)
	t.openWebAct = t.menu.AddActionWithText(trayOpenWeb)
	t.openWebAct.SetEnabled(false)
	t.menu.AddSeparator()
	t.quitAct = t.menu.AddActionWithText("Close")
	t.openAct.OnTriggered(func() { call(a.Open) })
	t.webAct.OnTriggered(func() { call(a.ToggleWeb) })
	t.openWebAct.OnTriggered(func() { call(a.OpenWeb) })
	t.quitAct.OnTriggered(func() { call(a.Quit) })
	t.icon.SetContextMenu(t.menu)
	t.icon.OnActivated(t.activated)
	return t
}

// activated opens the window on a click or a double click; the context menu and
// the middle button are left to the desktop.
func (t *Tray) activated(reason qt.QSystemTrayIcon__ActivationReason) {
	if reason == qt.QSystemTrayIcon__Trigger || reason == qt.QSystemTrayIcon__DoubleClick {
		call(t.open)
	}
}

// SetWebRunning follows the web server: the menu item offers the opposite of
// what is happening, and the page can be opened only while it runs.
func (t *Tray) SetWebRunning(running bool) {
	if running {
		t.webAct.SetText(trayStopWeb)
	} else {
		t.webAct.SetText(trayStartWeb)
	}
	t.openWebAct.SetEnabled(running)
}

// Notify shows a short message next to the icon, when the desktop can.
func (t *Tray) Notify(title, text string) {
	if qt.QSystemTrayIcon_SupportsMessages() {
		t.icon.ShowMessage4(title, text, qt.QSystemTrayIcon__Information)
	}
}

// Show puts the icon in the tray.
func (t *Tray) Show() { t.icon.Show() }

// Hide takes the icon out of the tray, so that it does not linger after the
// application has quit.
func (t *Tray) Hide() { t.icon.Hide() }

// The menu items, for tests.
func (t *Tray) OpenAction() *qt.QAction    { return t.openAct }
func (t *Tray) WebAction() *qt.QAction     { return t.webAct }
func (t *Tray) OpenWebAction() *qt.QAction { return t.openWebAct }
func (t *Tray) CloseAction() *qt.QAction   { return t.quitAct }
