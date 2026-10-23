package gui

import (
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/english-tutor/internal/view"
)

// Widget is the window's content. It rebuilds when the window's state changes.
func (w *MainWindow) Widget() widgets.Widget {
	return m.NewListenableBuilder(w.redraw, func(bc widgets.BuildContext, _ widgets.Widget) widgets.Widget {
		w.prefersDark = isDark(bc)
		w.navCtx = bc
		if size := m.MediaQuerySizeOf(bc); size.Width > 0 && size.Height > 0 {
			w.size = [2]int{int(size.Width), int(size.Height)}
		}
		return w.withShortcuts(m.NewScaffold(m.ScaffoldOpts{Body: w.content(bc)}))
	}, nil)
}

// withShortcuts binds the keyboard shortcuts of the window around its content.
func (w *MainWindow) withShortcuts(child widgets.Widget) widgets.Widget {
	var bindings []widgets.CallbackShortcutEntry
	for _, s := range w.shortcuts() {
		if a, ok := activatorFor(s.keys); ok {
			bindings = append(bindings, widgets.CallbackShortcutEntry{Activator: a, Callback: s.run})
		}
	}
	return m.NewCallbackShortcuts(bindings, child)
}

// activatorFor reads a key sequence such as "Ctrl+Shift+S".
func activatorFor(keys string) (widgets.ShortcutActivator, bool) {
	parts := strings.Split(keys, "+")
	o := widgets.SingleActivatorOpts{}
	for _, mod := range parts[:len(parts)-1] {
		switch mod {
		case "Ctrl":
			o.Control = true
		case "Shift":
			o.Shift = true
		case "Alt":
			o.Alt = true
		default:
			return nil, false
		}
	}
	key, ok := logicalKey(parts[len(parts)-1])
	if !ok {
		return nil, false
	}
	return widgets.NewSingleActivator(key, o), true
}

func logicalKey(name string) (services.LogicalKeyboardKey, bool) {
	switch name {
	case "Return":
		return services.LogicalKeyboardKeyEnter, true
	case "F7":
		return services.LogicalKeyboardKeyF7, true
	case "N":
		return services.LogicalKeyboardKeyKeyN, true
	case "O":
		return services.LogicalKeyboardKeyKeyO, true
	case "S":
		return services.LogicalKeyboardKeyKeyS, true
	}
	return 0, false
}

func (w *MainWindow) content(bc widgets.BuildContext) widgets.Widget {
	var rows []widgets.Widget
	if w.banner != "" {
		rows = append(rows, w.bannerBar(bc))
	}
	rows = append(rows,
		w.toolbar(bc),
		m.NewExpanded(w.hSplit.Widget(
			w.leftPane(bc),
			w.vSplit.Widget(w.issuesPane(bc), w.explainPane(bc)))),
		w.statusBar(bc))
	page := m.NewColumn(rows, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStretch})
	if len(w.panels) == 0 {
		return page
	}
	layers := []widgets.Widget{page}
	for _, p := range w.panels {
		layers = append(layers, m.NewPositioned(m.NewSizedBox(p.Build(bc), m.SizedBoxOpts{Width: foundation.Some(440.0), Height: foundation.Some(420.0)}),
			m.PositionedOpts{Right: foundation.Some(16.0), Bottom: foundation.Some(36.0)}))
	}
	return m.NewStack(layers, m.StackOpts{Fit: rendering.StackFitExpand})
}

// ---- Toolbars ----

func gap(width float64) widgets.Widget {
	return m.NewSizedBox(nil, m.SizedBoxOpts{Width: foundation.Some(width)})
}

func tip(message string, child widgets.Widget) widgets.Widget {
	return m.NewTooltip(child, m.TooltipOpts{Message: message})
}

func (w *MainWindow) toolbar(bc widgets.BuildContext) widgets.Widget {
	items := make([]*m.DropdownMenuItem[uint], len(w.llms))
	for i, c := range w.llms {
		items[i] = m.NewDropdownMenuItem[uint](m.NewText(c.Name), m.DropdownMenuItemOpts[uint]{Value: foundation.Some(c.ID)})
	}
	var onChanged func(foundation.Opt[uint])
	var value foundation.Opt[uint]
	if len(w.llms) > 0 {
		onChanged = func(v foundation.Opt[uint]) {
			if id, ok := v.Get(); ok {
				w.chooseLLM(id)
			}
		}
		value = foundation.Some(w.llms[max(w.selectedLLM, 0)].ID)
	}
	selector := m.NewDropdownButton(items, onChanged, m.DropdownButtonOpts[uint]{Value: value})

	var onCheck func()
	if w.checkEnabled() {
		onCheck = w.CheckButtonClick
	}
	check := tip("Find errors in the text, or cancel the running check ("+checkShortcut+")",
		m.NewFilledButton(onCheck, m.NewText(w.CheckButtonText())))
	auto := tip("Check again automatically when you pause typing. Off by default.",
		m.NewRow([]widgets.Widget{
			m.NewCheckbox(foundation.Some(w.autoCheck), func(v foundation.Opt[bool]) { w.ToggleAutoCheck(v.OrElse(false)) }),
			m.NewText("Auto-check"),
		}, m.RowOpts{MainAxisSize: m.MainAxisSizeMin}))
	button := func(label string, f func()) widgets.Widget { return m.NewOutlinedButton(f, m.NewText(label)) }
	web := tip(w.webTip, button(w.webText, w.WebButtonClick))

	first := m.NewWrap([]widgets.Widget{
		m.NewText("LLM:"), m.NewSizedBox(selector, m.SizedBoxOpts{Width: foundation.Some(220.0)}),
		check, auto, button("Statistics", w.StatisticsButtonClick), button("Preferences", w.PreferencesButtonClick), web,
	}, m.WrapOpts{Spacing: 10, RunSpacing: 6, CrossAxisAlignment: m.WrapCrossAlignmentCenter})

	var files []widgets.Widget
	for _, b := range fileButtons {
		btn := button(b.name, func() { w.FileButtonClick(b.name) })
		if b.keys != "" {
			btn = tip(b.name+" ("+b.keys+")", btn)
		}
		files = append(files, btn)
	}
	second := m.NewWrap(files, m.WrapOpts{Spacing: 10, RunSpacing: 6})
	return m.NewPadding(painting.NewEdgeInsetsSymmetric(6, 8), m.NewColumn([]widgets.Widget{
		first, m.NewSizedBox(nil, m.SizedBoxOpts{Height: foundation.Some(6.0)}), second,
	}, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStart}))
}

func (w *MainWindow) bannerBar(bc widgets.BuildContext) widgets.Widget {
	return m.NewColoredBox(ui.NewColor(0xFFFBE9DD), m.NewPadding(painting.NewEdgeInsetsSymmetric(4, 8), m.NewRow([]widgets.Widget{
		m.NewExpanded(m.NewSelectionArea(m.NewText(w.banner, m.TextOpts{Style: painting.NewTextStyle(painting.TextStyleOpts{
			Color: foundation.Some(ui.NewColor(0xFF7A2E00))})}))),
		tip("Dismiss", m.NewTextButton(w.ClearError, m.NewText("×"))),
	})))
}

// ---- Editor side ----

func (w *MainWindow) leftPane(bc widgets.BuildContext) widgets.Widget {
	dark := isDark(bc)
	legend := []widgets.Widget{m.NewText("Legend:")}
	for _, c := range legendOrder {
		on := w.legendOn[c]
		color := ui.NewColor(0xFF808080)
		weight := ui.FontWeightW400
		if on {
			color, weight = hexColor(view.CategoryColor(c, dark)), ui.FontWeightW700
		}
		label := m.NewText(view.CategorySample(c)+" "+c.Label(), m.TextOpts{Style: painting.NewTextStyle(painting.TextStyleOpts{
			Color: foundation.Some(color), FontWeight: weight})})
		legend = append(legend, tip("Click to hide or show the "+c.Label()+" underlines",
			m.NewTextButton(func() { w.LegendButtonClick(c) }, label)))
	}
	return m.NewColumn([]widgets.Widget{
		m.NewPadding(painting.NewEdgeInsetsSymmetric(0, 8), m.NewRow([]widgets.Widget{
			m.NewExpanded(m.NewWrap(legend, m.WrapOpts{Spacing: 4, CrossAxisAlignment: m.WrapCrossAlignmentCenter})),
			gap(8),
			tip("Remove all the text from the editor, like New (asks first if there are unsaved changes)",
				m.NewOutlinedButton(w.ClearButtonClick, m.NewText("Clear"))),
		})),
		m.NewExpanded(w.editor.Widget()),
	}, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStretch})
}

// ---- Issues list ----

func (w *MainWindow) issuesPane(bc widgets.BuildContext) widgets.Widget {
	theme := m.ThemeOf(bc)
	bold := w.resultsStyle(theme, theme.TextTheme.TitleSmall)
	var list widgets.Widget
	if len(w.rows) == 0 {
		list = m.NewPadding(painting.NewEdgeInsetsAll(8), m.NewText(w.emptyText, m.TextOpts{Style: w.resultsStyle(theme, theme.TextTheme.BodyMedium).CopyWith(
			painting.TextStyleCopy{Color: foundation.Some(ui.NewColor(0xFF808080))})}))
	} else {
		list = m.NewListViewBuilder(func(bc widgets.BuildContext, i int) widgets.Widget {
			return w.issueRow(bc, i)
		}, m.ListViewOpts{ItemCount: foundation.Some(len(w.rows))})
	}
	list = m.NewCallbackShortcuts([]widgets.CallbackShortcutEntry{
		{Activator: widgets.NewSingleActivator(services.LogicalKeyboardKeyArrowDown), Callback: func() { w.stepIssueRow(1) }},
		{Activator: widgets.NewSingleActivator(services.LogicalKeyboardKeyArrowUp), Callback: func() { w.stepIssueRow(-1) }},
	}, m.NewFocus(list, m.FocusOpts{FocusNode: w.listFocus}))
	return m.NewColumn([]widgets.Widget{
		m.NewPadding(painting.NewEdgeInsetsAll(6), m.NewText(w.issuesHeader, m.TextOpts{Style: bold})),
		m.NewExpanded(list),
	}, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStretch})
}

// issueRow is one row: the category letter in its colour and the snippet, and
// under them the error type and the count badge.
func (w *MainWindow) issueRow(bc widgets.BuildContext, i int) widgets.Widget {
	r := w.rows[i]
	theme := m.ThemeOf(bc)
	color := hexColor(view.CategoryColor(r.Category, isDark(bc)))
	grey := ui.NewColor(0xFF808080)
	bold := func(c ui.Color) *painting.TextStyle {
		return painting.NewTextStyle(painting.TextStyleOpts{Color: foundation.Some(c), FontWeight: ui.FontWeightW700})
	}
	second := []painting.InlineSpan{painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(r.Label),
		Style: painting.NewTextStyle(painting.TextStyleOpts{Color: foundation.Some(grey)})})}
	if b := r.Badge(); b != "" {
		second = append(second, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some("  " + b), Style: bold(theme.ColorScheme.OnSurface())}))
	}
	text := m.NewTextRich(painting.NewTextSpan(painting.TextSpanOpts{Style: w.resultsStyle(theme, theme.TextTheme.BodyMedium), Children: []painting.InlineSpan{
		painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(r.Marker), Style: bold(color)}),
		painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(" " + r.Snippet + "\n")}),
		painting.NewTextSpan(painting.TextSpanOpts{Children: second}),
	}}))
	selected := r.ID == w.selectedID
	var bg ui.Color
	if selected {
		bg = ui.NewColor(0x3C7F7F7F) // a pale selection keeps the marker and the grey line readable
	}
	body := m.NewDecoratedBox(painting.NewBoxDecoration(painting.BoxDecorationOpts{
		Color:  foundation.Some(bg),
		Border: leftBorder(selectedEdge(selected, theme), 3),
	}), m.NewPadding(painting.NewEdgeInsetsSymmetric(6, 8), text))
	// The selection follows the raw pointer-down, not a tap: a tap that competes
	// with the double tap is only decided after the double-tap time.
	return m.NewListener(m.NewGestureDetector(body, m.GestureDetectorOpts{
		Behavior:           rendering.HitTestBehaviorOpaque,
		OnDoubleTap:        func() { w.DoubleClickIssueRow(i) },
		OnSecondaryTapDown: func(d gestures.TapDownDetails) { w.showIssueMenu(bc, i, d.GlobalPosition) },
	}), widgets.ListenerOpts{
		OnPointerDown: func(ev *gestures.PointerDownEvent) {
			if ev.Buttons()&gestures.PrimaryButton != 0 {
				w.ClickIssueRow(i)
			}
		},
		// The text field drops the focus when a pointer goes down outside it, so the
		// list asks for the focus when the pointer comes up.
		OnPointerUp: func(*gestures.PointerUpEvent) { w.listFocus.RequestFocus() },
	})
}

// showIssueMenu opens the right-click menu of a row (section 4.3) at the mouse.
func (w *MainWindow) showIssueMenu(bc widgets.BuildContext, row int, at ui.Offset) {
	w.ClickIssueRow(row)
	items := make([]m.PopupMenuEntry, len(issueMenuLabels))
	for i, label := range issueMenuLabels {
		items[i] = m.NewPopupMenuItem[string](m.NewText(label), m.PopupMenuItemOpts[string]{
			Value: foundation.Some(label), OnTap: func() { w.TriggerIssueMenu(label) },
		})
	}
	m.ShowMenu(bc, items, m.ShowMenuOpts{Position: foundation.Some(rendering.NewRelativeRectFromLTRB(at.Dx, at.Dy, at.Dx, at.Dy))})
}

// ---- Explanation panel ----

func (w *MainWindow) explainPane(bc widgets.BuildContext) widgets.Widget {
	theme := m.ThemeOf(bc)
	c := view.ControlsFor(w.explainState)
	var col []widgets.Widget
	if c.Header {
		col = append(col, m.NewPadding(painting.NewEdgeInsetsAll(6), m.NewText(w.explainHeader, m.TextOpts{Style: w.resultsStyle(theme, theme.TextTheme.TitleSmall)})))
	}
	if c.Progress {
		col = append(col, m.NewLinearProgressIndicator())
	}
	var buttons []widgets.Widget
	if c.Redo {
		buttons = append(buttons, m.NewOutlinedButton(func() { w.ClickExplainButton("redo") }, m.NewText(c.RedoLabel)))
	}
	if c.Cancel {
		buttons = append(buttons, m.NewOutlinedButton(func() { w.ClickExplainButton("cancel") }, m.NewText("Cancel")))
	}
	if c.NotAnError {
		buttons = append(buttons, tip("The LLM was wrong: remove this issue and its count",
			m.NewOutlinedButton(func() { w.ClickExplainButton("notanerror") }, m.NewText("Not an error"))))
	}
	if len(buttons) > 0 {
		col = append(col, m.NewPadding(painting.NewEdgeInsetsSymmetric(4, 6), m.NewWrap(buttons, m.WrapOpts{Spacing: 8, RunSpacing: 4})))
	}
	w.disposeRecognizers()
	body := m.NewSingleChildScrollView(m.NewPadding(painting.NewEdgeInsetsAll(8),
		m.NewSelectionArea(w.markdownView(bc, parseMarkdown(w.explainBody)))), m.SingleChildScrollViewOpts{Controller: w.explainScroll})
	col = append(col, m.NewExpanded(body))
	return m.NewColumn(col, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStretch})
}

// markdownView draws the blocks. Links are tappable only here, and tapping one
// only asks the window, which decides (see onLink).
func (w *MainWindow) markdownView(bc widgets.BuildContext, blocks []mdBlock) widgets.Widget {
	theme := m.ThemeOf(bc)
	base := w.resultsStyle(theme, theme.TextTheme.BodyMedium)
	var out []widgets.Widget
	pad := func(child widgets.Widget) widgets.Widget {
		return m.NewPadding(painting.NewEdgeInsetsSymmetric(4, 0), child)
	}
	for _, b := range blocks {
		switch b.Kind {
		case blockParagraph:
			out = append(out, pad(m.NewTextRich(w.inlineSpan(bc, base, b.Inlines))))
		case blockHeading:
			style := w.resultsStyle(theme, theme.TextTheme.TitleMedium)
			if b.Level == 1 {
				style = w.resultsStyle(theme, theme.TextTheme.TitleLarge)
			}
			out = append(out, pad(m.NewTextRich(w.inlineSpan(bc, style, b.Inlines))))
		case blockCode:
			out = append(out, pad(m.NewContainer(m.NewText(b.Code, m.TextOpts{Style: mono(base)}), m.ContainerOpts{
				Padding: painting.NewEdgeInsetsAll(8), Color: foundation.Some(ui.NewColor(0x1F7F7F7F)),
			})))
		case blockRule:
			out = append(out, m.NewDivider())
		case blockQuote:
			out = append(out, pad(m.NewDecoratedBox(painting.NewBoxDecoration(painting.BoxDecorationOpts{
				Border: leftBorder(ui.NewColor(0xFF808080), 3),
			}), m.NewPadding(painting.NewEdgeInsetsSymmetric(0, 10), w.markdownView(bc, b.Blocks)))))
		case blockList:
			for i, item := range b.Items {
				mark := "•"
				if b.Ordered {
					mark = strconv.Itoa(i+1) + "."
				}
				out = append(out, m.NewPadding(painting.NewEdgeInsetsSymmetric(2, 12), m.NewRow([]widgets.Widget{
					m.NewSizedBox(m.NewText(mark, m.TextOpts{Style: base}), m.SizedBoxOpts{Width: foundation.Some(24.0)}),
					m.NewExpanded(m.NewTextRich(w.inlineSpan(bc, base, item))),
				}, m.RowOpts{CrossAxisAlignment: m.CrossAxisAlignmentStart})))
			}
		}
	}
	return m.NewColumn(out, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStart})
}

// pixels is a size in points as logical pixels.
func pixels(points int) float64 { return float64(points) * 4 / 3 }

// resultsStyle is a style of the theme in the font of the results: their family,
// and the size scaled so that body text has the chosen size and headings keep
// their proportion to it.
func (w *MainWindow) resultsStyle(theme *m.ThemeData, style *painting.TextStyle) *painting.TextStyle {
	f := w.fonts.Results
	scale := pixels(f.Size) / theme.TextTheme.BodyMedium.FontSize.OrElse(14)
	return style.CopyWith(painting.TextStyleCopy{FontFamily: f.Family, FontSize: foundation.Some(style.FontSize.OrElse(14) * scale)})
}

func mono(base *painting.TextStyle) *painting.TextStyle {
	return base.CopyWith(painting.TextStyleCopy{FontFamily: "monospace"})
}

// inlineSpan is a line of text with its bold, italic, code and link runs.
func (w *MainWindow) inlineSpan(bc widgets.BuildContext, base *painting.TextStyle, runs []mdInline) painting.InlineSpan {
	theme := m.ThemeOf(bc)
	children := make([]painting.InlineSpan, 0, len(runs))
	for _, r := range runs {
		o := painting.TextStyleOpts{}
		if r.Bold {
			o.FontWeight = ui.FontWeightW700
		}
		if r.Italic {
			o.FontStyle = ui.FontStyleItalic
		}
		if r.Code {
			o.FontFamily = "monospace"
			o.BackgroundColor = foundation.Some(ui.NewColor(0x1F7F7F7F))
		}
		so := painting.TextSpanOpts{Text: foundation.Some(r.Text), Style: painting.NewTextStyle(o)}
		if r.Link != "" && view.DecideLink(r.Link, w.explainState == view.ExplainIdle) != view.LinkIgnore {
			so.Style = painting.NewTextStyle(painting.TextStyleOpts{
				Color: foundation.Some(theme.ColorScheme.Primary()), Decoration: foundation.Some(ui.TextDecorationUnderline),
				FontWeight: o.FontWeight, FontStyle: o.FontStyle,
			})
			so.Recognizer = w.linkRecognizer(r.Link)
		}
		children = append(children, painting.NewTextSpan(so))
	}
	return painting.NewTextSpan(painting.TextSpanOpts{Style: base, Children: children})
}

// ---- Status bar ----

func (w *MainWindow) statusBar(bc widgets.BuildContext) widgets.Widget {
	style := m.ThemeOf(bc).TextTheme.BodySmall
	label := func(s string) widgets.Widget { return m.NewText(s, m.TextOpts{Style: style}) }
	return m.NewPadding(painting.NewEdgeInsetsSymmetric(4, 10), m.NewRow([]widgets.Widget{
		m.NewExpanded(label(w.status)), label(w.words), gap(16), label(w.tokens),
	}))
}

// leftBorder is a border of one side, the left.
func leftBorder(color ui.Color, width float64) painting.Border {
	return painting.Border{Left: painting.NewBorderSide(painting.BorderSideOpts{
		Color: foundation.Some(color), Width: foundation.Some(width)})}
}

// selectedEdge is the colour of the stripe at the left of a row: the accent for
// the selected row and nothing for the others.
func selectedEdge(selected bool, theme *m.ThemeData) ui.Color {
	if selected {
		return theme.ColorScheme.Primary()
	}
	return ui.NewColor(0x00000000)
}

// lifecycle puts the handling of the close button around child: the window asks
// the application what to do instead of closing.
func (w *MainWindow) lifecycle(_ widgets.BuildContext, child widgets.Widget) widgets.Widget {
	return &lifecycleHost{StatefulWidget: widgets.NewStatefulWidgetBase(), win: w, child: child}
}

type lifecycleHost struct {
	widgets.StatefulWidget
	win   *MainWindow
	child widgets.Widget
}

func (h *lifecycleHost) CreateState() widgets.State { return &lifecycleState{} }

type lifecycleState struct {
	widgets.StateOf[*lifecycleHost]
	listener *widgets.AppLifecycleListener
}

func (s *lifecycleState) InitState() {
	s.listener = widgets.NewAppLifecycleListener(widgets.WidgetsBindingOf(s.Context()), widgets.AppLifecycleListenerOpts{
		OnExitRequested: s.Widget().win.onExitRequested,
	})
}

func (s *lifecycleState) Dispose() { s.listener.Dispose() }

func (s *lifecycleState) Build(widgets.BuildContext) widgets.Widget { return s.Widget().child }

// onExitRequested is the close button. The application decides; the window stays
// unless it calls Exit.
func (w *MainWindow) onExitRequested() ui.AppExitResponse {
	if w.quitting || w.handlers.CloseRequested == nil {
		return ui.AppExitResponseExit
	}
	w.handlers.CloseRequested()
	return ui.AppExitResponseCancel
}
