package ui

import (
	qt "github.com/mappu/miqt/qt6"
)

// Tray is the system tray icon with its two-item menu: Open shows the main
// window and Close quits the application.
type Tray struct {
	icon    *qt.QSystemTrayIcon
	menu    *qt.QMenu
	openAct *qt.QAction
	quitAct *qt.QAction
	open    func()
}

// TrayAvailable reports whether the desktop has a system tray. Without one the
// application must not hide its window, because nothing could bring it back.
func TrayAvailable() bool { return qt.QSystemTrayIcon_IsSystemTrayAvailable() }

// NewTray builds the tray icon. open and quit run on the UI thread when the user
// chooses Open (or clicks the icon) and Close. The icon is not visible until
// Show is called.
func NewTray(open, quit func()) *Tray {
	t := &Tray{open: open}
	fallback := qt.QApplication_Style().StandardIcon(qt.QStyle__SP_FileIcon, nil, nil)
	t.icon = qt.NewQSystemTrayIcon2(qt.QIcon_FromTheme2("accessories-text-editor", fallback))
	t.icon.SetToolTip("English Tutor")
	t.menu = qt.NewQMenu2()
	t.openAct = t.menu.AddActionWithText("Open")
	t.quitAct = t.menu.AddActionWithText("Close")
	t.openAct.OnTriggered(func() { call(open) })
	t.quitAct.OnTriggered(func() { call(quit) })
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

// Show puts the icon in the tray.
func (t *Tray) Show() { t.icon.Show() }

// Hide takes the icon out of the tray, so that it does not linger after the
// application has quit.
func (t *Tray) Hide() { t.icon.Hide() }

// OpenAction and CloseAction are the menu items, for tests.
func (t *Tray) OpenAction() *qt.QAction  { return t.openAct }
func (t *Tray) CloseAction() *qt.QAction { return t.quitAct }
