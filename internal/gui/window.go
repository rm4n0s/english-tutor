package gui

import (
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/issues"
	"github.com/rm4n0s/english-tutor/internal/llm"
	"github.com/rm4n0s/english-tutor/internal/view"
)

// LLMChoice is one entry of the LLM selector.
type LLMChoice struct {
	ID   uint
	Name string
}

// Handlers are the actions the window reports. Every field is optional; the
// window does nothing for a nil one. They are called on the UI thread.
type Handlers struct {
	Check, Cancel func()
	Statistics    func()
	Preferences   func()
	// WebToggle starts or stops the web server (section 4.7).
	WebToggle        func()
	New, Open        func()
	Save, SaveAs     func()
	SaveToLibrary    func()
	Library          func()
	AutoCheckChanged func(on bool)
	LLMSelected      func(id uint)
	CategoryToggled  func(c analysis.Category, visible bool)
	TextChanged      func()

	// TextEdited reports every change of the text: at UTF-16 position pos,
	// removed units were replaced by added units.
	TextEdited func(pos, removed, added int)
	// CursorMoved reports the new text cursor position (UTF-16 units).
	CursorMoved func(pos int)
	// TooltipAt returns the tooltip for the text position under the mouse, or
	// "" for none.
	TooltipAt func(pos int) string
	// NextIssue and PrevIssue are F7 and Shift+F7.
	NextIssue, PrevIssue func()
	// Alternative is the Alternative entry of the editor's right-click menu: the
	// selection, as UTF-16 positions, wants other ways to write it.
	Alternative func(start, end int)
	// CheckSelection is the Check entry of the editor's right-click menu: only
	// the selection, as UTF-16 positions, is checked.
	CheckSelection func(start, end int)

	// ModifiedChanged reports when the text becomes modified or is marked saved.
	ModifiedChanged func(modified bool)

	// IssueSelected is a click or an arrow key on a row of the issues list.
	IssueSelected func(id int)
	// IssueActivated is a double click on a row.
	IssueActivated func(id int)
	// NotAnError and Regenerate act on the selected issue; ExplainCancel stops
	// a streaming explanation.
	NotAnError, Regenerate, ExplainCancel func()

	// CloseRequested is called when the user closes the window (or the desktop
	// asks it to close). The application decides: it hides the window (Hide),
	// ends the application (Exit) after asking about unsaved changes, or does
	// nothing. Without it the application ends.
	CloseRequested func()
}

// Hints shown in the explanation panel when it has no explanation.
const (
	hintIdle = "Select an error in the list or an underlined error in the text to learn more."
	// preferencesLink is the target of the link in the no-LLM message.
	preferencesLink = "preferences:"
	hintNoLLM       = "**No LLM is configured yet.**\n\nOpen [Preferences](" + preferencesLink + ") and add one to start checking your text."
)

// legendOrder is the order of the legend entries (section 4.1).
var legendOrder = []analysis.Category{analysis.Grammar, analysis.Vocabulary, analysis.Syntax}

// fileButtons are the buttons of the second toolbar row, with the keyboard
// shortcut of the ones that have one.
var fileButtons = []struct{ name, keys string }{
	{"New", "Ctrl+N"},
	{"Open", "Ctrl+O"},
	{"Save", "Ctrl+S"},
	{"Save As", "Ctrl+Shift+S"},
	{"Save to library", ""},
	{"Library", ""},
}

// checkShortcut presses the Check (or Cancel) button.
const checkShortcut = "Ctrl+Return"

// A check that takes longer than busyHintDelay shows busyHintText under the
// busy indicator, so the user knows they can do something else meanwhile.
const busyHintDelay = 10 * time.Second

// Web server button texts.
const (
	webStartText = "Start web server"
	webStopText  = "Stop web server"
)

// MainWindow is the application's main window (section 4.1). It holds the state
// of what is shown and reports user actions through Handlers; it contains no
// application logic. Its methods belong to the UI thread.
type MainWindow struct {
	handlers Handlers
	rt       *Runtime
	redraw   *foundation.ChangeNotifier // tells the widget tree to rebuild
	editor   *Editor

	title string

	llms        []LLMChoice
	selectedLLM int // index in llms, -1 for none
	autoCheck   bool
	checking    bool
	webText     string
	webTip      string

	legendOn map[analysis.Category]bool

	issuesHeader string
	rows         []issues.Row
	rowIDs       []int
	selectedID   int
	emptyText    string

	explainState  view.ExplainState
	explainHeader string
	explainBody   string
	confirmLink   func(url string) bool // asks before a link is opened; tests replace it
	openLink      func(url string)      // opens a confirmed link; tests replace it
	prefersDark   bool
	explainScroll widgets.ScrollController
	listFocus     *widgets.FocusNode
	navCtx        widgets.BuildContext // under the navigator that shows the dialogs
	panels        []panel              // floating windows that are not modal
	recognizers   []*gestures.TapGestureRecognizer

	status string
	words  string
	tokens string

	banner string

	hintDelay time.Duration
	stopHint  func()

	hSplit *splitter
	vSplit *splitter
	size   [2]int // the window size in logical pixels, as saved

	visible  bool
	active   bool
	alerted  int
	quitting bool
}

// NewMainWindow builds the window. Pass the runtime that makes it run on the UI
// thread; it may be nil when nothing needs a timer.
func NewMainWindow(rt *Runtime) *MainWindow {
	w := &MainWindow{
		rt:          rt,
		redraw:      foundation.NewChangeNotifier(),
		title:       "English Tutor",
		selectedLLM: -1,
		legendOn:    map[analysis.Category]bool{},
		webText:     webStartText,
		webTip:      webStartTip,
		hintDelay:   busyHintDelay,
		hSplit:      newSplitter(false, 2, 1),
		vSplit:      newSplitter(true, 1, 2),
		size:        [2]int{1100, 700},
		visible:     true,
	}
	for _, c := range legendOrder {
		w.legendOn[c] = true
	}
	w.editor = NewEditor(editorHooks{
		Edited: func(e Edit) {
			if w.handlers.TextEdited != nil {
				w.handlers.TextEdited(e.Pos, e.Removed, e.Added)
			}
		},
		TextChanged: func() {
			w.updateWordCount()
			call(w.handlers.TextChanged)
		},
		CursorMoved: func(pos int) {
			if w.handlers.CursorMoved != nil {
				w.handlers.CursorMoved(pos)
			}
		},
		ModifiedChanged: func(modified bool) {
			if w.handlers.ModifiedChanged != nil {
				w.handlers.ModifiedChanged(modified)
			}
		},
		TooltipAt: func(pos int) string {
			if w.handlers.TooltipAt == nil {
				return ""
			}
			return w.handlers.TooltipAt(pos)
		},
		Alternative: func(s, e int) {
			if w.handlers.Alternative != nil {
				w.handlers.Alternative(s, e)
			}
		},
		CheckSelection: func(s, e int) {
			if w.handlers.CheckSelection != nil {
				w.handlers.CheckSelection(s, e)
			}
		},
		CheckEnabled: func() bool { return len(w.llms) > 0 },
	})
	w.explainScroll = widgets.NewScrollController()
	w.listFocus = widgets.NewFocusNode()
	w.confirmLink = func(string) bool { return false }
	w.openLink = func(string) {}
	w.SetIssues("Issues (0)", nil, 0, "Press Check to find errors")
	w.SetLLMs(nil, 0) // disables the selector and Check
	w.ShowHint()      // shows the no-LLM message
	w.SetTokens(llm.Usage{}, llm.Usage{})
	w.status = "Ready"
	w.updateWordCount()
	return w
}

const webStartTip = "Serve this window in a web browser, on this computer or, with a password, on others."

// Dispose releases what the window holds.
func (w *MainWindow) Dispose() {
	if w.stopHint != nil {
		w.stopHint()
	}
	w.editor.Dispose()
	w.listFocus.Dispose()
	w.redraw.Dispose()
}

func (w *MainWindow) refresh() { w.redraw.NotifyListeners() }

func call(f func()) {
	if f != nil {
		f()
	}
}

// Editor returns the text field of the window.
func (w *MainWindow) Editor() *Editor { return w.editor }

// SetHandlers sets the actions the window reports.
func (w *MainWindow) SetHandlers(h Handlers) { w.handlers = h }

// ---- Window ----

// Show shows the window.
func (w *MainWindow) Show() { w.visible = true; w.refresh() }

// Present brings the window to the front: it shows it if it is hidden, restores
// it if it is minimised and asks the desktop to focus it.
func (w *MainWindow) Present() {
	w.visible, w.active = true, true
	if c := w.windowController(); c != nil {
		c.Activate()
	}
	w.refresh()
}

// Visible reports whether the window is shown.
func (w *MainWindow) Visible() bool { return w.visible }

// Close asks the window to close, as the user's close button does: the
// application's CloseRequested handler decides what happens. Without one the
// application ends.
func (w *MainWindow) Close() {
	if w.handlers.CloseRequested != nil {
		w.handlers.CloseRequested()
		return
	}
	w.Exit()
}

// Hide takes the window off the screen and the task bar, and Present brings it
// back. The application goes on, and the window keeps its state.
func (w *MainWindow) Hide() {
	w.visible = false
	if c := w.windowController(); c != nil {
		c.SetHidden(true)
	}
	w.refresh()
}

// Exit ends the application.
func (w *MainWindow) Exit() {
	w.quitting = true
	w.visible = false
	if w.rt != nil {
		if b := w.rt.Binding(); b != nil {
			b.ExitApplication(ui.AppExitTypeRequired)
		}
	}
}

// Quitting reports whether Exit was called.
func (w *MainWindow) Quitting() bool { return w.quitting }

// windowController returns the controller of the window of the app, or nil
// before it runs.
func (w *MainWindow) windowController() *widgets.RegularWindowController {
	if w.rt == nil || w.rt.Binding() == nil {
		return nil
	}
	c, _ := w.rt.Binding().WindowingOwner().ImplicitWindowController()
	return c
}

// SetTitle sets the window title.
func (w *MainWindow) SetTitle(title string) { w.title = title; w.refresh() }

// Title returns the window title.
func (w *MainWindow) Title() string { return w.title }

// IsActive reports whether the window is the one the user is working in. It is
// false when another application has the focus and when the window is hidden.
func (w *MainWindow) IsActive() bool { return w.active && w.visible }

// SetActive records that the desktop gave or took the focus of the window.
func (w *MainWindow) SetActive(active bool) { w.active = active }

// Alert asks the desktop to draw attention to the window when the user is in
// another application.
func (w *MainWindow) Alert() { w.alerted++ }

// Alerts returns how many times the window asked for attention.
func (w *MainWindow) Alerts() int { return w.alerted }

// Geometry returns the window size in a form Geometry restores: "WxH". The
// desktop, not the application, decides where a window goes.
func (w *MainWindow) Geometry() []byte {
	return []byte(formatSize(w.size))
}

// SetGeometry restores a geometry saved by Geometry. Empty or damaged data is
// ignored.
func (w *MainWindow) SetGeometry(g []byte) {
	if size, ok := parseSize(string(g)); ok {
		w.size = size
	}
}

// WindowSize returns the size the window should open with.
func (w *MainWindow) WindowSize() (width, height int) { return w.size[0], w.size[1] }

// SetWindowSize records the size of the window, for Geometry.
func (w *MainWindow) SetWindowSize(width, height int) {
	if width > 0 && height > 0 {
		w.size = [2]int{width, height}
	}
}

// SplitterSizes returns the sizes of the two splitters as one list: the
// horizontal splitter (editor, right column) then the vertical splitter
// (issues, explanation).
func (w *MainWindow) SplitterSizes() []int {
	h, v := w.hSplit.Sizes(), w.vSplit.Sizes()
	if len(h) != 2 || len(v) != 2 {
		return nil
	}
	return append(h, v...)
}

// SetSplitterSizes restores sizes saved by SplitterSizes. Anything that is not
// four positive numbers is ignored.
func (w *MainWindow) SetSplitterSizes(sizes []int) {
	if len(sizes) != 4 {
		return
	}
	for _, s := range sizes {
		if s <= 0 {
			return
		}
	}
	w.hSplit.SetSizes(sizes[:2])
	w.vSplit.SetSizes(sizes[2:])
}

// ---- Toolbar ----

// SetWebRunning shows whether the web server runs: the button offers the
// opposite, and its tooltip gives the address of the page.
func (w *MainWindow) SetWebRunning(running bool, url string) {
	if running {
		w.webText = webStopText
		w.webTip = "The web server runs at " + url + " . Press to stop it."
	} else {
		w.webText = webStartText
		w.webTip = webStartTip
	}
	w.refresh()
}

// WebButtonText returns the text of the web server button.
func (w *MainWindow) WebButtonText() string { return w.webText }

// SetLLMs fills the selector and selects the entry with the given ID (or the
// first one if it is not in the list). With no entries the selector and the
// Check button are disabled and the explanation panel tells the user to open
// Preferences.
func (w *MainWindow) SetLLMs(choices []LLMChoice, selected uint) {
	w.llms = append([]LLMChoice(nil), choices...)
	w.selectedLLM = -1
	if len(choices) > 0 {
		w.selectedLLM = 0
	}
	for i, c := range choices {
		if c.ID == selected {
			w.selectedLLM = i
		}
	}
	if w.explainState == view.ExplainIdle {
		w.ShowHint()
	}
	w.refresh()
}

// SelectedLLMID returns the ID of the selected LLM, or 0 if there is none.
func (w *MainWindow) SelectedLLMID() uint {
	if w.selectedLLM < 0 || w.selectedLLM >= len(w.llms) {
		return 0
	}
	return w.llms[w.selectedLLM].ID
}

// chooseLLM is the user picking an entry of the selector.
func (w *MainWindow) chooseLLM(id uint) {
	for i, c := range w.llms {
		if c.ID == id {
			w.selectedLLM = i
			if w.handlers.LLMSelected != nil {
				w.handlers.LLMSelected(id)
			}
			w.refresh()
			return
		}
	}
}

// SelectLLM picks an entry of the selector as the user would.
func (w *MainWindow) SelectLLM(id uint) { w.chooseLLM(id) }

// SetAutoCheck sets the Auto-check checkbox without reporting a change.
func (w *MainWindow) SetAutoCheck(on bool) { w.autoCheck = on; w.refresh() }

// AutoCheck reports whether the Auto-check checkbox is ticked.
func (w *MainWindow) AutoCheck() bool { return w.autoCheck }

// ToggleAutoCheck ticks or unticks the checkbox as the user would.
func (w *MainWindow) ToggleAutoCheck(on bool) {
	w.autoCheck = on
	if w.handlers.AutoCheckChanged != nil {
		w.handlers.AutoCheckChanged(on)
	}
	w.refresh()
}

// SetChecking turns the Check button into a Cancel button while a check runs,
// and back.
func (w *MainWindow) SetChecking(checking bool) { w.checking = checking; w.refresh() }

// checkEnabled says whether the Check button can be pressed.
func (w *MainWindow) checkEnabled() bool { return len(w.llms) > 0 || w.checking }

// CheckButtonText returns the text of the Check / Cancel button.
func (w *MainWindow) CheckButtonText() string {
	if w.checking {
		return "Cancel"
	}
	return "Check"
}

// CheckButtonClick presses the Check (or Cancel) button.
func (w *MainWindow) CheckButtonClick() {
	if !w.checkEnabled() {
		return
	}
	if w.checking {
		call(w.handlers.Cancel)
	} else {
		call(w.handlers.Check)
	}
}

// CheckButtonEnabled reports whether the Check button can be pressed.
func (w *MainWindow) CheckButtonEnabled() bool { return w.checkEnabled() }

// StatisticsButtonClick presses the Statistics button.
func (w *MainWindow) StatisticsButtonClick() { call(w.handlers.Statistics) }

// PreferencesButtonClick presses the Preferences button, as the user would.
func (w *MainWindow) PreferencesButtonClick() { call(w.handlers.Preferences) }

// WebButtonClick presses the web server button.
func (w *MainWindow) WebButtonClick() { call(w.handlers.WebToggle) }

// FileButtonClick presses a button of the second row by its label.
func (w *MainWindow) FileButtonClick(name string) {
	h := map[string]func(){
		"New": w.handlers.New, "Open": w.handlers.Open, "Save": w.handlers.Save, "Save As": w.handlers.SaveAs,
		"Save to library": w.handlers.SaveToLibrary, "Library": w.handlers.Library,
	}
	call(h[name])
}

// ClearButtonClick presses the Clear button above the editor. Clearing the text
// is starting a new one, so it is handled as New: it asks about unsaved changes
// and detaches the text from its file.
func (w *MainWindow) ClearButtonClick() { call(w.handlers.New) }

// LegendButtonClick clicks the legend entry of a category.
func (w *MainWindow) LegendButtonClick(c analysis.Category) {
	on, ok := w.legendOn[c]
	if !ok {
		return
	}
	w.legendOn[c] = !on
	if w.handlers.CategoryToggled != nil {
		w.handlers.CategoryToggled(c, !on)
	}
	w.refresh()
}

// LegendChecked reports whether the underlines of a category are shown.
func (w *MainWindow) LegendChecked(c analysis.Category) bool { return w.legendOn[c] }

// ---- Shortcuts ----

type shortcut struct {
	keys string
	run  func()
}

// shortcuts are the keyboard shortcuts of the window.
func (w *MainWindow) shortcuts() []shortcut {
	list := []shortcut{{checkShortcut, w.CheckButtonClick}}
	for _, b := range fileButtons {
		if b.keys != "" {
			list = append(list, shortcut{b.keys, func() { w.FileButtonClick(b.name) }})
		}
	}
	return append(list,
		shortcut{"F7", func() { call(w.handlers.NextIssue) }},
		shortcut{"Shift+F7", func() { call(w.handlers.PrevIssue) }})
}

// ShortcutKeys returns the key sequences of the shortcuts that press buttons.
func (w *MainWindow) ShortcutKeys() []string {
	var out []string
	for _, s := range w.shortcuts() {
		if s.keys != "F7" && s.keys != "Shift+F7" {
			out = append(out, s.keys)
		}
	}
	return out
}

// ActivateShortcut triggers the shortcut with the given key sequence, as the
// user pressing it would. It reports whether there is one.
func (w *MainWindow) ActivateShortcut(keys string) bool {
	for _, s := range w.shortcuts() {
		if s.keys == keys {
			s.run()
			return true
		}
	}
	return false
}

// ---- Editor ----

// SetEditorBusy puts the loading screen over the editor, or takes it away. While
// it is up the text cannot be edited, because the result of the check in
// progress is for the text as it is.
func (w *MainWindow) SetEditorBusy(busy bool) {
	if w.stopHint != nil {
		w.stopHint()
		w.stopHint = nil
	}
	w.editor.SetBusy(busy)
	if busy && w.rt != nil {
		w.stopHint = w.rt.After(w.hintDelay, w.editor.ShowBusyHint)
	}
}

// EditorBusy reports whether the loading screen is over the editor.
func (w *MainWindow) EditorBusy() bool { return w.editor.Busy() }

// SetFontSize sets the editor font size in points.
func (w *MainWindow) SetFontSize(points int) { w.editor.SetFontSize(points) }

// FontSize returns the editor font size in points.
func (w *MainWindow) FontSize() int { return w.editor.FontSize() }

// Text returns the text in the editor.
func (w *MainWindow) Text() string { return w.editor.Text() }

// SetText replaces the text in the editor.
func (w *MainWindow) SetText(text string) { w.editor.SetText(text) }

func (w *MainWindow) updateWordCount() {
	w.words = view.FormatWords(analysis.WordCount(w.editor.Text()))
	w.refresh()
}

// SetUnderlines replaces the underlines of the editor.
func (w *MainWindow) SetUnderlines(us []Underline) { w.editor.SetUnderlines(us) }

// Underlines returns the underlines last set.
func (w *MainWindow) Underlines() []Underline { return w.editor.Underlines() }

// CursorPosition returns the text cursor position in UTF-16 units.
func (w *MainWindow) CursorPosition() int { return w.editor.CursorPosition() }

// MoveCursorTo puts the text cursor at pos and scrolls it into view. It does not
// take keyboard focus away from the widget that has it.
func (w *MainWindow) MoveCursorTo(pos int) { w.editor.MoveCursorTo(pos) }

// SelectRange selects the text between the UTF-16 positions start and end.
func (w *MainWindow) SelectRange(start, end int) { w.editor.SelectRange(start, end) }

// TypeIntoEditor types text at the end of the editor, as the user would; a
// read-only editor ignores it.
func (w *MainWindow) TypeIntoEditor(text string) { w.editor.TypeIntoEditor(text) }

// ReplaceRange replaces the text between the UTF-16 positions start and end with
// text, and puts the cursor after it. It reports the edit like typing: issues
// touched by it go.
func (w *MainWindow) ReplaceRange(start, end int, text string) {
	w.editor.ReplaceRange(start, end, text)
}

// InsertTextAt inserts text at the UTF-16 position pos, as if it were typed.
func (w *MainWindow) InsertTextAt(pos int, text string) { w.editor.InsertTextAt(pos, text) }

// Undo undoes the last edit of the editor.
func (w *MainWindow) Undo() { w.editor.Undo() }

// FocusEditor gives the keyboard focus to the editor.
func (w *MainWindow) FocusEditor() { w.editor.Focus() }

// Modified reports whether the text has changed since it was loaded or saved.
func (w *MainWindow) Modified() bool { return w.editor.Modified() }

// SetModified marks the text as changed or as saved, without changing it.
func (w *MainWindow) SetModified(modified bool) { w.editor.SetModified(modified) }

// ClearEditor empties the editor and marks it as saved.
func (w *MainWindow) ClearEditor() { w.editor.ClearEditor() }

// LoadText replaces the text of the editor with text loaded from somewhere and
// marks it as saved, with the cursor at the start and the undo history empty.
func (w *MainWindow) LoadText(text string) { w.editor.LoadText(text) }

// EditorMenuLabels returns the labels of the editor's right-click menu as it is
// now (it depends on the selection).
func (w *MainWindow) EditorMenuLabels() []string {
	var out []string
	for _, it := range w.editor.MenuItems() {
		if l, ok := it.Label.Get(); ok {
			out = append(out, l)
		}
	}
	return out
}

// TriggerEditorMenu triggers one entry of the editor's right-click menu by its
// label, and reports whether the menu has it.
func (w *MainWindow) TriggerEditorMenu(label string) bool {
	for _, it := range w.editor.MenuItems() {
		if l, _ := it.Label.Get(); l == label {
			if it.OnPressed != nil {
				it.OnPressed()
			}
			return true
		}
	}
	return false
}

// EditorMenuEnabled reports whether the editor's right-click menu has an entry
// with the label and it can be chosen.
func (w *MainWindow) EditorMenuEnabled(label string) bool {
	for _, it := range w.editor.MenuItems() {
		if l, _ := it.Label.Get(); l == label {
			return it.OnPressed != nil
		}
	}
	return false
}

// ---- Status bar and banner ----

// SetStatus sets the left part of the status bar (check state and counts).
func (w *MainWindow) SetStatus(text string) { w.status = text; w.refresh() }

// StatusText returns the left part of the status bar.
func (w *MainWindow) StatusText() string { return w.status }

// SetTokens shows the token usage of the last request and of the session.
func (w *MainWindow) SetTokens(last, session llm.Usage) {
	w.tokens = view.FormatTokens(last, session)
	w.refresh()
}

// TokensText returns the token part of the status bar.
func (w *MainWindow) TokensText() string { return w.tokens }

// WordsText returns the word count of the status bar.
func (w *MainWindow) WordsText() string { return w.words }

// ShowError shows a message in the banner above the editor. It stays until the
// user dismisses it or ClearError is called.
func (w *MainWindow) ShowError(msg string) { w.banner = msg; w.refresh() }

// ClearError hides the banner.
func (w *MainWindow) ClearError() { w.banner = ""; w.refresh() }

// ErrorText returns the text of the error banner, or "" if it is hidden.
func (w *MainWindow) ErrorText() string { return w.banner }

// ---- Formatting helpers ----

func formatSize(s [2]int) string {
	return strconv.Itoa(s[0]) + "x" + strconv.Itoa(s[1])
}

func parseSize(s string) ([2]int, bool) {
	a, b, ok := strings.Cut(s, "x")
	if !ok {
		return [2]int{}, false
	}
	x, errX := strconv.Atoi(a)
	y, errY := strconv.Atoi(b)
	if errX != nil || errY != nil || x <= 0 || y <= 0 {
		return [2]int{}, false
	}
	return [2]int{x, y}, true
}

// panel is a floating window that does not block the main window.
type panel interface {
	Build(bc widgets.BuildContext) widgets.Widget
	onClosed()
}

// showPanel puts p over the corner of the window.
func (w *MainWindow) showPanel(p panel) {
	w.panels = append(w.panels, p)
	if d, ok := p.(*AlternativesDialog); ok {
		d.open = true
	}
	w.refresh()
}

// hidePanel takes p away and tells it.
func (w *MainWindow) hidePanel(p panel) {
	for i, q := range w.panels {
		if q == p {
			w.panels = append(w.panels[:i], w.panels[i+1:]...)
			break
		}
	}
	p.onClosed()
	w.refresh()
}
