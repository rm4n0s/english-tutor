package gui

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"path/filepath"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/desktop"
	"github.com/rm4n0s/glutter/foundation"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// appID is the application ID: the Wayland app_id and the X11 WM_CLASS.
const appID = "dev.rm4n0s.english-tutor"

// AppID returns the application ID, for RunApp.
func AppID() string { return appID }

// TrayActions are what the tray menu does. A nil one leaves its item out.
type TrayActions struct {
	// Open brings the window back.
	Open func()
	// Quit ends the application.
	Quit func()
	// ToggleWeb starts or stops the web server, and OpenWeb opens its page.
	ToggleWeb func()
	OpenWeb   func()
}

// Tray is the icon of the application in the system tray. It lives while the
// widget returned by Wrap is on screen. If no desktop shows tray icons, glutter
// reports the error TrayUnavailable and the application goes on without one
// (see Available).
type Tray struct {
	actions    TrayActions
	icon       ui.WindowIcon
	iconErr    error
	webRunning bool
	hidden     bool
	redraw     *foundation.ChangeNotifier

	unavailable bool
	onGone      func()
	ctx         widgets.BuildContext // under the tray, for notifications
	notifyCtx   context.Context
	cancel      context.CancelFunc
	// notify shows a desktop message; tests replace it.
	notify func(title, text string)
}

// NewTray makes the tray icon with its menu.
func NewTray(a TrayActions) *Tray {
	t := &Tray{actions: a, redraw: foundation.NewChangeNotifier()}
	t.notifyCtx, t.cancel = context.WithCancel(context.Background())
	t.icon, t.iconErr = trayIcon()
	t.notify = t.desktopNotify
	return t
}

// Available reports whether the desktop shows the icon. It is true until glutter
// reports that it cannot.
func (t *Tray) Available() bool { return !t.unavailable && t.iconErr == nil }

// SetUnavailableHandler sets the function called once when it turns out that
// there is no tray on this desktop.
func (t *Tray) SetUnavailableHandler(f func()) { t.onGone = f }

// MarkUnavailable records that the desktop shows no tray icon.
func (t *Tray) MarkUnavailable() {
	if t.unavailable {
		return
	}
	t.unavailable = true
	t.hidden = true
	t.redraw.NotifyListeners()
	call(t.onGone)
}

// IsTrayUnavailable reports whether err is glutter's report that there is no tray.
func IsTrayUnavailable(err error) bool {
	appErr, ok := errors.FromError(err)
	return ok && appErr.Tag == "TrayUnavailable"
}

// SetWebRunning shows in the menu whether the web server runs.
func (t *Tray) SetWebRunning(running bool) {
	t.webRunning = running
	t.redraw.NotifyListeners()
}

// Show puts the icon in the tray and Hide takes it away.
func (t *Tray) Show() { t.hidden = false; t.redraw.NotifyListeners() }
func (t *Tray) Hide() { t.hidden = true; t.redraw.NotifyListeners() }

// Notify shows a message from the desktop.
func (t *Tray) Notify(title, text string) { t.notify(title, text) }

func (t *Tray) desktopNotify(title, text string) {
	bc := t.ctx
	if bc == nil {
		return
	}
	go func() {
		_, _ = desktop.ShowNotification(t.notifyCtx, bc, title, desktop.NotificationOpts{
			Body: text, Icon: "dialog-information",
			Actions:  []desktop.NotificationAction{{ID: "default", Label: "Open"}},
			OnAction: func(string) { call(t.actions.Open) },
		})
	}()
}

type trayItem struct {
	label string
	run   func()
}

// items are the entries of the menu; a nil run is a separator.
func (t *Tray) items() []trayItem {
	web := webStartText
	if t.webRunning {
		web = webStopText
	}
	var out []trayItem
	add := func(label string, f func()) {
		if f != nil {
			out = append(out, trayItem{label, f})
		}
	}
	add(TrayOpenLabel, t.actions.Open)
	add(web, t.actions.ToggleWeb)
	if t.webRunning {
		add(TrayOpenWebLabel, t.actions.OpenWeb)
	}
	out = append(out, trayItem{})
	add(TrayCloseLabel, t.actions.Quit)
	return out
}

// MenuLabels returns the labels of the menu items, for tests.
func (t *Tray) MenuLabels() []string {
	var out []string
	for _, it := range t.items() {
		if it.run != nil {
			out = append(out, it.label)
		}
	}
	return out
}

// Choose runs the menu item with the label, and reports whether there is one.
func (t *Tray) Choose(label string) bool {
	for _, it := range t.items() {
		if it.run != nil && it.label == label {
			it.run()
			return true
		}
	}
	return false
}

// Menu labels.
const (
	TrayOpenLabel    = "Open English Tutor"
	TrayCloseLabel   = "Close"
	TrayOpenWebLabel = "Open web page"
)

func (t *Tray) menu() []desktop.TrayMenuEntry {
	var out []desktop.TrayMenuEntry
	for _, it := range t.items() {
		if it.run == nil {
			out = append(out, desktop.NewTrayMenuSeparator())
		} else {
			out = append(out, desktop.NewTrayMenuItem(it.label, it.run))
		}
	}
	return out
}

// Wrap puts the tray icon around the window's widget.
func (t *Tray) Wrap(child widgets.Widget) widgets.Widget {
	return m.NewListenableBuilder(t.redraw, func(bc widgets.BuildContext, _ widgets.Widget) widgets.Widget {
		t.ctx = bc
		if t.hidden || t.iconErr != nil {
			return child
		}
		return desktop.NewSystemTray(t.icon, child, desktop.SystemTrayOpts{
			Title: "English Tutor", Tooltip: "English Tutor",
			OnActivate: t.actions.Open,
			Menu:       t.menu(),
		})
	}, nil)
}

// trayIcon draws the icon of the tray: a green disc with a white page on it.
func trayIcon() (ui.WindowIcon, error) {
	const size = 64
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	green := color.NRGBA{R: 0x00, G: 0x72, B: 0x5E, A: 0xFF}
	for y := range size {
		for x := range size {
			dx, dy := float64(x)+0.5-size/2, float64(y)+0.5-size/2
			if dx*dx+dy*dy <= (size/2-2)*(size/2-2) {
				img.SetNRGBA(x, y, green)
			}
		}
	}
	white := image.NewUniform(color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF})
	draw.Draw(img, image.Rect(20, 16, 44, 48), white, image.Point{}, draw.Src)
	for line := range 3 {
		y := 22 + line*8
		draw.Draw(img, image.Rect(24, y, 40, y+2), image.NewUniform(green), image.Point{}, draw.Src)
	}
	return desktop.TrayIconFromRGBA(size, img.Pix)
}

// ---- Files and links ----

// fileFilters are the filters of the file dialogs.
var fileFilters = []desktop.FileFilter{
	{Name: "Text files", Patterns: []string{"*.txt", "*.md", "*.TXT", "*.MD"}},
	{Name: "All files", Patterns: []string{"*"}},
}

// ChooseOpenFile asks for a text file to open. done is called on the UI thread
// with ok false when the user cancels or the desktop has no file chooser.
func (w *MainWindow) ChooseOpenFile(done func(path string, ok bool)) {
	w.chooseFile(func(ctx context.Context, bc widgets.BuildContext) (desktop.FilesResult, error) {
		return desktop.OpenFiles(ctx, bc, desktop.OpenFilesOpts{Title: "Open text", Filters: fileFilters})
	}, done)
}

// ChooseSaveFile asks where to save; suggested is the proposed file name or path.
func (w *MainWindow) ChooseSaveFile(suggested string, done func(path string, ok bool)) {
	w.chooseFile(func(ctx context.Context, bc widgets.BuildContext) (desktop.FilesResult, error) {
		return desktop.SaveFile(ctx, bc, desktop.SaveFileOpts{
			Title: "Save text", Filters: fileFilters,
			CurrentName: filepath.Base(suggested), CurrentFolder: dirOf(suggested),
		})
	}, done)
}

func dirOf(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Dir(path)
	}
	return ""
}

func (w *MainWindow) chooseFile(ask func(ctx context.Context, bc widgets.BuildContext) (desktop.FilesResult, error), done func(string, bool)) {
	bc := w.navCtx
	if bc == nil {
		done("", false)
		return
	}
	go func() {
		res, err := ask(context.Background(), bc)
		w.post(func() {
			if err != nil {
				w.ShowError("The file chooser failed: " + userMessage(err))
				done("", false)
				return
			}
			if res.Cancelled || len(res.Paths) == 0 {
				done("", false)
				return
			}
			done(res.Paths[0], true)
		})
	}()
}

// OpenURL opens an address in the browser.
func (w *MainWindow) OpenURL(url string) {
	bc := w.navCtx
	if bc == nil {
		return
	}
	go func() {
		if err := desktop.OpenURI(context.Background(), bc, url); err != nil {
			w.post(func() { w.ShowError("The link could not be opened: " + userMessage(err)) })
		}
	}()
}

// ---- The application ----

// Root is the widget to run: a Material app around the main window, with the tray
// icon and the handling of the close button. The theme follows the system's light
// or dark setting.
func Root(win *MainWindow, tray *Tray, rt *Runtime) widgets.Widget {
	seed := ui.NewColor(0xFF00725E)
	home := m.NewBuilder(func(bc widgets.BuildContext) widgets.Widget {
		rt.Attach(widgets.WidgetsBindingOf(bc))
		return win.lifecycle(bc, win.Widget())
	})
	var content widgets.Widget = home
	if tray != nil {
		content = tray.Wrap(home)
	}
	return m.NewMaterialApp(m.MaterialAppOpts{
		Title:                      "English Tutor",
		Theme:                      m.NewThemeData(m.ThemeDataOpts{ColorSchemeSeed: m.Some(seed), Brightness: ui.BrightnessLight}),
		DarkTheme:                  m.NewThemeData(m.ThemeDataOpts{ColorSchemeSeed: m.Some(seed), Brightness: ui.BrightnessDark}),
		DebugShowCheckedModeBanner: m.Some(false),
		Home:                       content,
	})
}
