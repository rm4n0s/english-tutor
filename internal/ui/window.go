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
	AutoCheckChanged func(on bool)
	LLMSelected      func(id uint)
	CategoryToggled  func(c analysis.Category, visible bool)
	TextChanged      func()

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
	explain      *qt.QTextBrowser

	stateLabel  *qt.QLabel
	wordsLabel  *qt.QLabel
	tokensLabel *qt.QLabel

	checking      bool
	showingHint   bool
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
	w.SetIssuesEmpty("Issues (0)", "Press Check to find errors")
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
	w.checkBtn.SetToolTip("Find errors in the text (a check always runs when you press this)")
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
	for _, name := range []string{"New", "Open", "Save", "Save As", "Save to library"} {
		b := qt.NewQPushButton3(name)
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
	issuesLayout.AddWidget(w.issuesList.QWidget)

	w.explain = qt.NewQTextBrowser2()
	// Links are never opened by Qt itself: they come from an LLM (section 5.9).
	w.explain.SetOpenLinks(false)
	w.explain.SetOpenExternalLinks(false)

	w.vSplit = qt.NewQSplitter3(qt.Vertical)
	w.vSplit.AddWidget(issues)
	w.vSplit.AddWidget(w.explain.QWidget)
	w.vSplit.SetStretchFactor(0, 1)
	w.vSplit.SetStretchFactor(1, 2)
	w.vSplit.SetChildrenCollapsible(false)

	w.hSplit = qt.NewQSplitter3(qt.Horizontal)
	w.hSplit.AddWidget(left)
	w.hSplit.AddWidget(w.vSplit.QWidget)
	w.hSplit.SetStretchFactor(0, 2)
	w.hSplit.SetStretchFactor(1, 1)
	w.hSplit.SetChildrenCollapsible(false)
	w.win.SetCentralWidget(w.hSplit.QWidget)
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
	w.explain.OnAnchorClicked(func(u *qt.QUrl) {
		// Only the link of the no-LLM message is followed. Links in LLM
		// explanations are handled with a confirmation in step 13.
		if u.Scheme() == "preferences" {
			call(w.handlers.Preferences)
		}
	})
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
	if w.showingHint {
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

// SetIssuesEmpty shows the header and a single grey line in place of rows.
func (w *MainWindow) SetIssuesEmpty(header, line string) {
	w.issuesHeader.SetText(header)
	w.issuesList.Clear()
	w.issuesList.AddItem(line)
	if item := w.issuesList.Item(0); item != nil {
		item.SetFlags(qt.NoItemFlags)
	}
}

// SetStatus sets the left part of the status bar (check state and counts).
func (w *MainWindow) SetStatus(text string) { w.stateLabel.SetText(text) }

// SetTokens shows the token usage of the last request and of the session.
func (w *MainWindow) SetTokens(last, session llm.Usage) {
	w.tokensLabel.SetText(FormatTokens(last, session))
}

// ShowMarkdown shows an explanation in the right panel.
func (w *MainWindow) ShowMarkdown(md string) {
	w.showingHint = false
	w.explain.SetMarkdown(md)
}

// ShowHint shows the idle hint, or the no-LLM message if no LLM is configured.
func (w *MainWindow) ShowHint() {
	w.explain.SetMarkdown(w.hintText())
	w.showingHint = true
}

func (w *MainWindow) hintText() string {
	if len(w.llms) == 0 {
		return hintNoLLM
	}
	return hintIdle
}

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
