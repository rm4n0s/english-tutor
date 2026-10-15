package ui

import (
	"fmt"

	qt "github.com/mappu/miqt/qt6"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/llm"
)

// LLMChoice is one entry of the LLM selector.
type LLMChoice struct {
	ID   uint
	Name string
}

// Handlers are the actions the window reports. Every field is optional; the
// window does nothing for a nil one. They are called on the UI thread.
type Handlers struct {
	Check, Cancel    func()
	Statistics       func()
	Preferences      func()
	New, Open        func()
	Save, SaveAs     func()
	SaveToLibrary    func()
	Library          func()
	AutoCheckChanged func(on bool)
	LLMSelected      func(id uint)
	CategoryToggled  func(c analysis.Category, visible bool)
	TextChanged      func()

	// TextEdited reports every change of the text: at UTF-16 position pos,
	// removed units were replaced by added units (QTextDocument's
	// contentsChange).
	TextEdited func(pos, removed, added int)
	// CursorMoved reports the new text cursor position (UTF-16 units).
	CursorMoved func(pos int)
	// TooltipAt returns the tooltip for the text position under the mouse, or
	// "" for none.
	TooltipAt func(pos int) string
	// NextIssue and PrevIssue are F7 and Shift+F7.
	NextIssue, PrevIssue func()

	// ModifiedChanged reports when the text becomes modified or is marked saved.
	ModifiedChanged func(modified bool)

	// IssueSelected is a click or an arrow key on a row of the issues list.
	IssueSelected func(id int)
	// IssueActivated is a double click on a row.
	IssueActivated func(id int)
	// NotAnError and Regenerate act on the selected issue; ExplainCancel stops
	// a streaming explanation.
	NotAnError, Regenerate, ExplainCancel func()

	// CanClose is asked when the user closes the window; returning false keeps
	// it open (for the unsaved-changes prompt).
	CanClose func() bool
	// Closed is called once the window has been told to close, before it goes
	// away, to save the window state.
	Closed func()
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

// fileShortcuts are the keyboard shortcuts of the toolbar buttons that have one.
var fileShortcuts = map[string]string{
	"New":     "Ctrl+N",
	"Open":    "Ctrl+O",
	"Save":    "Ctrl+S",
	"Save As": "Ctrl+Shift+S",
}

// checkShortcut presses the Check (or Cancel) button.
const checkShortcut = "Ctrl+Return"

// MainWindow is the application's main window (section 4.1). It holds widgets
// and reports user actions through Handlers; it contains no application logic.
type MainWindow struct {
	win      *qt.QMainWindow
	handlers Handlers

	llmCombo  *qt.QComboBox
	llms      []LLMChoice
	checkBtn  *qt.QPushButton
	autoCheck *qt.QCheckBox
	statsBtn  *qt.QPushButton
	prefsBtn  *qt.QPushButton
	fileBtns  []*qt.QPushButton

	legend map[analysis.Category]*qt.QToolButton

	editor       *qt.QPlainTextEdit
	hSplit       *qt.QSplitter
	vSplit       *qt.QSplitter
	issuesHeader *qt.QLabel
	issuesList   *qt.QListWidget
	rowIDs       []int // issue ID of each list row; empty while the list shows its empty line
	syncingList  bool  // true while the list is changed from code, so it reports nothing

	explain       *qt.QTextBrowser
	explainHeader *qt.QLabel
	explainBar    *qt.QProgressBar
	redoBtn       *qt.QPushButton
	cancelBtn     *qt.QPushButton
	notErrorBtn   *qt.QPushButton
	explainState  ExplainState
	confirmLink   func(url string) bool // asks before a link is opened; tests replace it
	openLink      func(url string)      // opens a confirmed link; tests replace it

	stateLabel  *qt.QLabel
	wordsLabel  *qt.QLabel
	tokensLabel *qt.QLabel

	banner      *qt.QWidget
	bannerLabel *qt.QLabel

	underlines   []Underline
	nextShortcut *qt.QShortcut
	prevShortcut *qt.QShortcut
	shortcuts    []*qt.QShortcut

	checking      bool
	fontSizePoint int
}

// NewMainWindow builds the window. It must be called on the UI thread after the
// QApplication exists.
func NewMainWindow() *MainWindow {
	w := &MainWindow{win: qt.NewQMainWindow2(), legend: map[analysis.Category]*qt.QToolButton{}}
	w.win.SetWindowTitle("English Tutor")
	w.win.Resize(1100, 700)
	w.buildToolbars()
	w.buildCentral()
	w.buildStatusBar()
	w.connect()
	w.SetIssues("Issues (0)", nil, 0, "Press Check to find errors")
	w.SetLLMs(nil, 0) // disables the selector and Check
	w.ShowHint()      // shows the no-LLM message
	w.SetTokens(llm.Usage{}, llm.Usage{})
	w.updateWordCount()
	return w
}

func (w *MainWindow) buildToolbars() {
	row1 := w.win.AddToolBarWithTitle("Check")
	row1.SetMovable(false)
	row1.AddWidget(qt.NewQLabel3("LLM:").QWidget)
	w.llmCombo = qt.NewQComboBox2()
	w.llmCombo.SetMinimumContentsLength(18)
	row1.AddWidget(w.llmCombo.QWidget)
	w.checkBtn = qt.NewQPushButton3("Check")
	w.checkBtn.SetToolTip("Find errors in the text, or cancel the running check (" + checkShortcut + ")")
	row1.AddWidget(w.checkBtn.QWidget)
	w.autoCheck = qt.NewQCheckBox3("Auto-check")
	w.autoCheck.SetToolTip("Check again automatically when you pause typing. Off by default.")
	row1.AddWidget(w.autoCheck.QWidget)
	row1.AddSeparator()
	w.statsBtn = qt.NewQPushButton3("Statistics")
	row1.AddWidget(w.statsBtn.QWidget)
	w.prefsBtn = qt.NewQPushButton3("Preferences")
	row1.AddWidget(w.prefsBtn.QWidget)

	w.win.AddToolBarBreak()
	row2 := w.win.AddToolBarWithTitle("Files")
	row2.SetMovable(false)
	for _, name := range []string{"New", "Open", "Save", "Save As", "Save to library", "Library"} {
		b := qt.NewQPushButton3(name)
		if keys, ok := fileShortcuts[name]; ok {
			b.SetToolTip(name + " (" + keys + ")")
		}
		w.fileBtns = append(w.fileBtns, b)
		row2.AddWidget(b.QWidget)
	}
}

func (w *MainWindow) buildCentral() {
	// Left: legend above the editor.
	left := qt.NewQWidget2()
	leftLayout := qt.NewQVBoxLayout2()
	leftLayout.SetContentsMargins(0, 0, 0, 0)
	left.SetLayout(leftLayout.QLayout)

	legendRow := qt.NewQHBoxLayout2()
	legendRow.AddWidget(qt.NewQLabel3("Legend:").QWidget)
	dark := w.isDark()
	for _, c := range legendOrder {
		b := qt.NewQToolButton2()
		b.SetText(CategorySample(c) + " " + c.Label())
		b.SetCheckable(true)
		b.SetChecked(true)
		b.SetToolTip(fmt.Sprintf("Click to hide or show the %s underlines", c.Label()))
		b.SetStyleSheet(fmt.Sprintf(
			"QToolButton { color: %s; font-weight: bold; border: none; padding: 2px 6px; }"+
				"QToolButton:!checked { color: gray; font-weight: normal; }", CategoryColor(c, dark)))
		w.legend[c] = b
		legendRow.AddWidget(b.QWidget)
	}
	legendRow.AddStretch()
	leftLayout.AddLayout(legendRow.QLayout)

	w.editor = qt.NewQPlainTextEdit2()
	w.editor.SetPlaceholderText("Write or paste your English text here...")
	w.editor.SetLineWrapMode(qt.QPlainTextEdit__WidgetWidth)
	leftLayout.AddWidget(w.editor.QWidget)

	// Right: issues list above the explanation panel.
	issues := qt.NewQWidget2()
	issuesLayout := qt.NewQVBoxLayout2()
	issuesLayout.SetContentsMargins(0, 0, 0, 0)
	issues.SetLayout(issuesLayout.QLayout)
	w.issuesHeader = qt.NewQLabel3("")
	w.issuesHeader.SetStyleSheet("font-weight: bold;")
	issuesLayout.AddWidget(w.issuesHeader.QWidget)
	w.issuesList = qt.NewQListWidget2()
	w.issuesList.SetContextMenuPolicy(qt.CustomContextMenu)
	// A pale selection keeps the coloured marker and the grey second line of a
	// row readable, which a solid highlight colour would not.
	w.issuesList.SetStyleSheet("QListWidget::item:selected { background: rgba(127, 127, 127, 60); border-left: 3px solid palette(highlight); }")
	issuesLayout.AddWidget(w.issuesList.QWidget)

	explainPanel := w.buildExplainPanel()

	w.vSplit = qt.NewQSplitter3(qt.Vertical)
	w.vSplit.AddWidget(issues)
	w.vSplit.AddWidget(explainPanel)
	w.vSplit.SetStretchFactor(0, 1)
	w.vSplit.SetStretchFactor(1, 2)
	w.vSplit.SetChildrenCollapsible(false)

	w.hSplit = qt.NewQSplitter3(qt.Horizontal)
	w.hSplit.AddWidget(left)
	w.hSplit.AddWidget(w.vSplit.QWidget)
	w.hSplit.SetStretchFactor(0, 2)
	w.hSplit.SetStretchFactor(1, 1)
	w.hSplit.SetChildrenCollapsible(false)

	// A non-modal banner for errors sits above everything else (section 10).
	w.banner = qt.NewQWidget2()
	bannerLayout := qt.NewQHBoxLayout2()
	bannerLayout.SetContentsMargins(8, 4, 8, 4)
	w.banner.SetLayout(bannerLayout.QLayout)
	w.bannerLabel = qt.NewQLabel3("")
	w.bannerLabel.SetWordWrap(true)
	w.bannerLabel.SetTextInteractionFlags(qt.TextSelectableByMouse)
	dismiss := qt.NewQToolButton2()
	dismiss.SetText("×")
	dismiss.SetToolTip("Dismiss")
	dismiss.OnClicked(w.ClearError)
	bannerLayout.AddWidget2(w.bannerLabel.QWidget, 1)
	bannerLayout.AddWidget(dismiss.QWidget)
	w.banner.SetStyleSheet("QWidget { background: #FBE9DD; color: #7A2E00; } QToolButton { border: none; font-weight: bold; }")
	w.banner.Hide()

	central := qt.NewQWidget2()
	centralLayout := qt.NewQVBoxLayout2()
	centralLayout.SetContentsMargins(0, 0, 0, 0)
	centralLayout.SetSpacing(0)
	central.SetLayout(centralLayout.QLayout)
	centralLayout.AddWidget(w.banner)
	centralLayout.AddWidget2(w.hSplit.QWidget, 1)
	w.win.SetCentralWidget(central)
}

func (w *MainWindow) buildStatusBar() {
	bar := w.win.StatusBar()
	w.stateLabel = qt.NewQLabel3("Ready")
	w.wordsLabel = qt.NewQLabel3("")
	w.tokensLabel = qt.NewQLabel3("")
	bar.AddWidget2(w.stateLabel.QWidget, 1)
	bar.AddPermanentWidget(w.wordsLabel.QWidget)
	bar.AddPermanentWidget(w.tokensLabel.QWidget)
}

func (w *MainWindow) connect() {
	w.checkBtn.OnClicked(func() {
		if w.checking {
			call(w.handlers.Cancel)
		} else {
			call(w.handlers.Check)
		}
	})
	w.statsBtn.OnClicked(func() { call(w.handlers.Statistics) })
	w.prefsBtn.OnClicked(func() { call(w.handlers.Preferences) })
	for i, f := range []func() func(){
		func() func() { return w.handlers.New },
		func() func() { return w.handlers.Open },
		func() func() { return w.handlers.Save },
		func() func() { return w.handlers.SaveAs },
		func() func() { return w.handlers.SaveToLibrary },
		func() func() { return w.handlers.Library },
	} {
		w.fileBtns[i].OnClicked(func() { call(f()) })
	}
	w.autoCheck.OnToggled(func(on bool) {
		if w.handlers.AutoCheckChanged != nil {
			w.handlers.AutoCheckChanged(on)
		}
	})
	w.llmCombo.OnActivated(func(index int) {
		if index >= 0 && index < len(w.llms) && w.handlers.LLMSelected != nil {
			w.handlers.LLMSelected(w.llms[index].ID)
		}
	})
	for c, b := range w.legend {
		b.OnToggled(func(on bool) {
			if w.handlers.CategoryToggled != nil {
				w.handlers.CategoryToggled(c, on)
			}
		})
	}
	w.editor.OnTextChanged(func() {
		w.updateWordCount()
		call(w.handlers.TextChanged)
	})
	w.editor.Document().OnContentsChange(func(pos, removed, added int) {
		if w.handlers.TextEdited != nil {
			w.handlers.TextEdited(pos, removed, added)
		}
	})
	w.editor.Document().OnModificationChanged(func(m bool) {
		if w.handlers.ModifiedChanged != nil {
			w.handlers.ModifiedChanged(m)
		}
	})
	w.editor.OnCursorPositionChanged(func() {
		if w.handlers.CursorMoved != nil {
			w.handlers.CursorMoved(w.editor.TextCursor().Position())
		}
	})
	w.editor.Viewport().SetMouseTracking(true)
	w.editor.OnViewportEvent(w.viewportEvent)
	w.addShortcuts()
	w.nextShortcut = qt.NewQShortcut2(qt.NewQKeySequence2("F7"), w.win.QObject)
	w.nextShortcut.OnActivated(func() { call(w.handlers.NextIssue) })
	w.prevShortcut = qt.NewQShortcut2(qt.NewQKeySequence2("Shift+F7"), w.win.QObject)
	w.prevShortcut.OnActivated(func() { call(w.handlers.PrevIssue) })
	w.connectPanel()
	w.win.OnCloseEvent(func(super func(event *qt.QCloseEvent), event *qt.QCloseEvent) {
		if w.handlers.CanClose != nil && !w.handlers.CanClose() {
			event.Ignore()
			return
		}
		call(w.handlers.Closed)
		super(event)
	})
}

func call(f func()) {
	if f != nil {
		f()
	}
}

func (w *MainWindow) isDark() bool {
	return w.win.Palette().Window().Color().Lightness() < 128
}

// PreferencesButtonClick presses the Preferences button, as the user would.
// It exists for tests of the code that sets the handlers.
func (w *MainWindow) PreferencesButtonClick() { w.prefsBtn.Click() }

// SetHandlers sets the actions the window reports.
func (w *MainWindow) SetHandlers(h Handlers) { w.handlers = h }

// Show shows the window.
func (w *MainWindow) Show() { w.win.Show() }

// Close closes the window as if the user had closed it.
func (w *MainWindow) Close() bool { return w.win.Close() }

// Widget returns the window as a QWidget, to parent dialogs.
func (w *MainWindow) Widget() *qt.QWidget { return w.win.QWidget }

// SetTitle sets the window title.
func (w *MainWindow) SetTitle(title string) { w.win.SetWindowTitle(title) }

// SetLLMs fills the selector and selects the entry with the given ID (or the
// first one if it is not in the list). With no entries the selector and the
// Check button are disabled and the explanation panel tells the user to open
// Preferences.
func (w *MainWindow) SetLLMs(choices []LLMChoice, selected uint) {
	w.llms = append([]LLMChoice(nil), choices...)
	w.llmCombo.Clear()
	current := 0
	for i, c := range choices {
		w.llmCombo.AddItem(c.Name)
		if c.ID == selected {
			current = i
		}
	}
	empty := len(choices) == 0
	w.llmCombo.SetEnabled(!empty)
	w.checkBtn.SetEnabled(!empty || w.checking)
	if !empty {
		w.llmCombo.SetCurrentIndex(current)
	}
	if w.explainState == ExplainIdle {
		w.ShowHint()
	}
}

// SelectedLLMID returns the ID of the selected LLM, or 0 if there is none.
func (w *MainWindow) SelectedLLMID() uint {
	i := w.llmCombo.CurrentIndex()
	if i < 0 || i >= len(w.llms) {
		return 0
	}
	return w.llms[i].ID
}

// SetAutoCheck sets the Auto-check checkbox without reporting a change.
func (w *MainWindow) SetAutoCheck(on bool) {
	blocked := w.autoCheck.BlockSignals(true)
	w.autoCheck.SetChecked(on)
	w.autoCheck.BlockSignals(blocked)
}

// AutoCheck reports whether the Auto-check checkbox is ticked.
func (w *MainWindow) AutoCheck() bool { return w.autoCheck.IsChecked() }

// SetChecking turns the Check button into a Cancel button while a check runs,
// and back.
func (w *MainWindow) SetChecking(checking bool) {
	w.checking = checking
	if checking {
		w.checkBtn.SetText("Cancel")
		w.checkBtn.SetEnabled(true)
		return
	}
	w.checkBtn.SetText("Check")
	w.checkBtn.SetEnabled(len(w.llms) > 0)
}

// SetFontSize sets the editor font size in points.
func (w *MainWindow) SetFontSize(points int) {
	if points < 1 {
		return
	}
	w.fontSizePoint = points
	f := w.editor.Font()
	f.SetPointSize(points)
	w.editor.SetFont(f)
}

// FontSize returns the editor font size in points.
func (w *MainWindow) FontSize() int { return w.editor.Font().PointSize() }

// Text returns the text in the editor.
func (w *MainWindow) Text() string { return w.editor.ToPlainText() }

// SetText replaces the text in the editor.
func (w *MainWindow) SetText(text string) { w.editor.SetPlainText(text) }

func (w *MainWindow) updateWordCount() {
	w.wordsLabel.SetText(FormatWords(analysis.WordCount(w.editor.ToPlainText())))
}

// SetStatus sets the left part of the status bar (check state and counts).
func (w *MainWindow) SetStatus(text string) { w.stateLabel.SetText(text) }

// SetTokens shows the token usage of the last request and of the session.
func (w *MainWindow) SetTokens(last, session llm.Usage) {
	w.tokensLabel.SetText(FormatTokens(last, session))
}

// ShowHint shows the idle hint, or the no-LLM message if no LLM is configured.
func (w *MainWindow) ShowHint() { w.SetExplanation(ExplainIdle, "", "") }

// Geometry returns the window geometry in Qt's saved form.
func (w *MainWindow) Geometry() []byte { return w.win.SaveGeometry() }

// SetGeometry restores a geometry saved by Geometry. Empty or damaged data is
// ignored.
func (w *MainWindow) SetGeometry(g []byte) {
	if len(g) > 0 {
		w.win.RestoreGeometry(g)
	}
}

// SplitterSizes returns the sizes of the two splitters as one list: the
// horizontal splitter (editor, right column) then the vertical splitter
// (issues, explanation).
func (w *MainWindow) SplitterSizes() []int {
	return append(w.hSplit.Sizes(), w.vSplit.Sizes()...)
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

// Underline is one underlined range of the text, in UTF-16 units. A selected
// underline is also drawn with a background of the category colour at about 20%
// opacity (section 4.2).
type Underline struct {
	Start, End int
	Category   analysis.Category
	Selected   bool
}

// selectedAlpha is 20% of 255.
const selectedAlpha = 51

// SetUnderlines replaces the underlines of the editor. They are extra
// selections, so they never change the text or the undo stack. The selected
// one is added last so that its background is drawn over the others.
func (w *MainWindow) SetUnderlines(us []Underline) {
	w.underlines = append([]Underline(nil), us...)
	dark := w.isDark()
	ordered := make([]Underline, 0, len(us))
	for _, u := range us {
		if !u.Selected {
			ordered = append(ordered, u)
		}
	}
	for _, u := range us {
		if u.Selected {
			ordered = append(ordered, u)
		}
	}

	var sels []qt.QTextEdit__ExtraSelection
	var cleanup []func()
	for _, u := range ordered {
		cur := qt.NewQTextCursor2(w.editor.Document())
		cur.SetPosition(u.Start)
		cur.SetPosition2(u.End, qt.QTextCursor__KeepAnchor)
		color := qt.NewQColor6(CategoryColor(u.Category, dark))
		format := qt.NewQTextCharFormat()
		format.SetUnderlineStyle(underlineStyle(CategoryStyle(u.Category)))
		format.SetUnderlineColor(color)
		if u.Selected {
			bg := qt.NewQColor6(CategoryColor(u.Category, dark))
			bg.SetAlpha(selectedAlpha)
			brush := qt.NewQBrush3(bg)
			format.SetBackground(brush)
			cleanup = append(cleanup, brush.Delete, bg.Delete)
		}
		sel := qt.NewQTextEdit__ExtraSelection2()
		sel.SetCursor(*cur)
		sel.SetFormat(*format)
		sels = append(sels, *sel)
		cleanup = append(cleanup, sel.Delete, format.Delete, color.Delete, cur.Delete)
	}
	w.editor.SetExtraSelections(sels)
	for _, c := range cleanup {
		c()
	}
}

// Underlines returns the underlines last set, for tests and for callers that
// need to know what is drawn.
func (w *MainWindow) Underlines() []Underline { return append([]Underline(nil), w.underlines...) }

func underlineStyle(s UnderlineStyle) qt.QTextCharFormat__UnderlineStyle {
	switch s {
	case UnderlineDash:
		return qt.QTextCharFormat__DashUnderline
	case UnderlineDot:
		return qt.QTextCharFormat__DotLine
	default:
		return qt.QTextCharFormat__WaveUnderline
	}
}

// viewportEvent answers the editor's tooltip events from the handler.
func (w *MainWindow) viewportEvent(super func(ev *qt.QEvent) bool, ev *qt.QEvent) bool {
	if ev.Type() != qt.QEvent__ToolTip || w.handlers.TooltipAt == nil {
		return super(ev)
	}
	help := qt.UnsafeNewQHelpEvent(ev.UnsafePointer())
	pos := w.editor.CursorForPosition(help.Pos()).Position()
	if text := w.handlers.TooltipAt(pos); text != "" {
		qt.QToolTip_ShowText2(help.GlobalPos(), text, w.editor.QWidget)
	} else {
		qt.QToolTip_HideText()
		ev.Ignore()
	}
	return true
}

// CursorPosition returns the text cursor position in UTF-16 units.
func (w *MainWindow) CursorPosition() int { return w.editor.TextCursor().Position() }

// MoveCursorTo puts the text cursor at pos and scrolls it into view. It does
// not take keyboard focus away from the widget that has it.
func (w *MainWindow) MoveCursorTo(pos int) {
	c := w.editor.TextCursor()
	c.SetPosition(pos)
	w.editor.SetTextCursor(c)
	w.editor.EnsureCursorVisible()
}

// FocusEditor gives the keyboard focus to the editor.
func (w *MainWindow) FocusEditor() { w.editor.SetFocus() }

// ShowError shows a message in the banner above the editor. It stays until the
// user dismisses it or ClearError is called.
func (w *MainWindow) ShowError(msg string) {
	w.bannerLabel.SetText(msg)
	w.banner.Show()
}

// ClearError hides the banner.
func (w *MainWindow) ClearError() {
	w.bannerLabel.SetText("")
	w.banner.Hide()
}

// The methods below let tests of the code that sets the handlers act as the
// user and read what the window shows.

// CheckButtonClick presses the Check (or Cancel) button.
func (w *MainWindow) CheckButtonClick() { w.checkBtn.Click() }

// CheckButtonText returns the text of the Check / Cancel button.
func (w *MainWindow) CheckButtonText() string { return w.checkBtn.Text() }

// LegendButtonClick clicks the legend entry of a category.
func (w *MainWindow) LegendButtonClick(c analysis.Category) {
	if b := w.legend[c]; b != nil {
		b.Click()
	}
}

// InsertTextAt inserts text at the UTF-16 position pos, as if it were typed.
func (w *MainWindow) InsertTextAt(pos int, text string) {
	c := w.editor.TextCursor()
	c.SetPosition(pos)
	c.InsertText(text)
}

// StatusText returns the left part of the status bar.
func (w *MainWindow) StatusText() string { return w.stateLabel.Text() }

// TokensText returns the token part of the status bar.
func (w *MainWindow) TokensText() string { return w.tokensLabel.Text() }

// ErrorText returns the text of the error banner, or "" if it is hidden.
func (w *MainWindow) ErrorText() string {
	if w.banner.IsHidden() {
		return ""
	}
	return w.bannerLabel.Text()
}

// StatisticsButtonClick presses the Statistics button.
func (w *MainWindow) StatisticsButtonClick() { w.statsBtn.Click() }

// Modified reports whether the text has changed since it was loaded or saved.
func (w *MainWindow) Modified() bool { return w.editor.Document().IsModified() }

// SetModified marks the text as changed or as saved, without changing it.
func (w *MainWindow) SetModified(modified bool) {
	w.editor.Document().SetModifiedWithBool(modified)
}

// ClearEditor empties the editor and marks it as saved. The issue state sees
// the change like any other edit.
func (w *MainWindow) ClearEditor() {
	w.editor.Clear()
	w.SetModified(false)
}

// LoadText replaces the text of the editor with text loaded from somewhere and
// marks it as saved, with the cursor at the start and the undo history empty.
func (w *MainWindow) LoadText(text string) {
	w.editor.SetPlainText(text)
	w.editor.Document().ClearUndoRedoStacks()
	w.SetModified(false)
	w.MoveCursorTo(0)
}

// addShortcuts creates the shortcuts that press the toolbar buttons.
func (w *MainWindow) addShortcuts() {
	press := map[string]*qt.QPushButton{checkShortcut: w.checkBtn}
	for i, name := range []string{"New", "Open", "Save", "Save As"} {
		press[fileShortcuts[name]] = w.fileBtns[i]
	}
	for keys, button := range press {
		sc := qt.NewQShortcut2(qt.NewQKeySequence2(keys), w.win.QObject)
		sc.OnActivated(func() { button.Click() })
		w.shortcuts = append(w.shortcuts, sc)
	}
}

// ShortcutKeys returns the key sequences of the shortcuts that press buttons.
func (w *MainWindow) ShortcutKeys() []string {
	var out []string
	for _, sc := range w.shortcuts {
		out = append(out, sc.Key().ToString())
	}
	return out
}

// ActivateShortcut triggers the shortcut with the given key sequence, as the
// user pressing it would. It reports whether there is one.
func (w *MainWindow) ActivateShortcut(keys string) bool {
	for _, sc := range w.shortcuts {
		if sc.Key().ToString() == keys {
			sc.Activated()
			return true
		}
	}
	return false
}
