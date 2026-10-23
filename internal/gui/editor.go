package gui

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/store"
	"github.com/rm4n0s/english-tutor/internal/view"
)

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

// The entries the editor's right-click menu gets when text is selected
// (section 4.2).
const (
	alternativeLabel = "Alternative"
	checkLabel       = "Check"
)

// editorHooks are what the editor reports. Every field is optional.
type editorHooks struct {
	// Edited reports every change of the text, as UTF-16 units.
	Edited func(Edit)
	// TextChanged follows Edited, after the word count and the like are due.
	TextChanged func()
	// CursorMoved reports the caret, in UTF-16 units.
	CursorMoved func(pos int)
	// ModifiedChanged reports when the text becomes modified or is marked saved.
	ModifiedChanged func(modified bool)
	// TooltipAt returns the tooltip for a text position, or "" for none.
	TooltipAt func(pos int) string
	// Alternative and CheckSelection are the entries of the right-click menu; the
	// range is in UTF-16 units. CheckEnabled says whether Check can be chosen.
	Alternative, CheckSelection func(start, end int)
	CheckEnabled                func() bool
}

// Editor is the text field of the main window: a multi-line EditableText with
// underlines, a loading screen and a tooltip. Its text and positions are the
// application's (UTF-16 units); glutter's own are bytes, and the editor converts.
// It holds no application logic and reports what happens through its hooks.
type Editor struct {
	hooks editorHooks

	controller *widgets.TextEditingController
	focus      *widgets.FocusNode
	undo       *widgets.UndoHistoryController
	fieldKey   *widgets.GlobalKey[*widgets.EditableTextState]
	gestures   *widgets.TextSelectionGestureDetectorBuilder
	changes    *foundation.ChangeNotifier // tells the widget to rebuild
	listener   foundation.ListenerHandle

	text      string // the text as of the last change, to find what an edit did
	units     *analysis.UTF16Map
	caret     int // the last caret reported, in UTF-16 units
	modified  bool
	font      store.Font
	busy      bool
	busyHint  bool
	placeholt string

	underlines []Underline

	hoverText string
	hoverAt   ui.Offset
}

// NewEditor makes the editor with the hooks it reports to.
func NewEditor(hooks editorHooks) *Editor {
	e := &Editor{
		hooks:      hooks,
		controller: widgets.NewTextEditingController(),
		focus:      widgets.NewFocusNode(),
		undo:       widgets.NewUndoHistoryController(),
		fieldKey:   widgets.NewGlobalKey[*widgets.EditableTextState](),
		changes:    foundation.NewChangeNotifier(),
		font:       store.Font{Size: store.DefaultFontSize},
		placeholt:  "Write or paste your English text here...",
		units:      analysis.NewUTF16Map(""),
	}
	e.gestures = widgets.NewTextSelectionGestureDetectorBuilder(e)
	e.controller.SetBuildTextSpan(e.buildSpan)
	e.listener = e.controller.AddListener(e.onControllerChanged)
	return e
}

// EditableTextKey, ForcePressEnabled and SelectionEnabled make the editor the
// delegate of the gesture detector, which turns clicks and drags into a caret and
// a selection.
func (e *Editor) EditableTextKey() *widgets.GlobalKey[*widgets.EditableTextState] {
	return e.fieldKey
}
func (e *Editor) ForcePressEnabled() bool { return false }
func (e *Editor) SelectionEnabled() bool  { return true }

// Dispose releases the controllers.
func (e *Editor) Dispose() {
	e.controller.RemoveListener(e.listener)
	e.controller.Dispose()
	e.undo.Dispose()
	e.focus.Dispose()
	e.changes.Dispose()
}

func (e *Editor) refresh() { e.changes.NotifyListeners() }

// onControllerChanged finds out what changed in the controller: the text (an
// edit), the caret, or the modified flag.
func (e *Editor) onControllerChanged() {
	v := e.controller.Value()
	if v.Text != e.text {
		edit := DiffText(e.text, v.Text, v.Selection.ExtentOffset)
		e.text = v.Text
		e.units = analysis.NewUTF16Map(v.Text)
		if !e.modified {
			e.modified = true
			if e.hooks.ModifiedChanged != nil {
				e.hooks.ModifiedChanged(true)
			}
		}
		if e.hooks.Edited != nil {
			e.hooks.Edited(edit)
		}
		if e.hooks.TextChanged != nil {
			e.hooks.TextChanged()
		}
	}
	if pos := e.CursorPosition(); pos != e.caret {
		e.caret = pos
		if e.hooks.CursorMoved != nil {
			e.hooks.CursorMoved(pos)
		}
	}
}

// toUnits and toBytes convert between glutter's byte offsets and UTF-16 units.
func (e *Editor) toUnits(b int) int { return e.units.Offset(b) }
func (e *Editor) toBytes(u int) int { return e.units.ByteOffset(u) }

// Text returns the text.
func (e *Editor) Text() string { return e.controller.Text() }

// setValue puts text and a collapsed caret (a byte offset) in the controller.
func (e *Editor) setValue(text string, caretByte int) {
	e.controller.SetValue(services.NewTextEditingValue(services.TextEditingValueOpts{
		Text: text, Selection: foundation.Some(services.NewTextSelectionCollapsed(caretByte)),
	}))
}

// SetText replaces the text, as setting the whole text of a field does: the
// caret goes to the end, the text counts as saved and the undo history starts
// again. Use TypeIntoEditor for text that the user types.
func (e *Editor) SetText(text string) {
	e.setValue(text, len(text))
	e.fieldKey = widgets.NewGlobalKey[*widgets.EditableTextState]()
	e.setModified(false)
	e.refresh()
}

// LoadText replaces the text with text loaded from somewhere and marks it as
// saved, with the caret at the start and the undo history empty.
func (e *Editor) LoadText(text string) {
	e.setValue(text, 0)
	e.fieldKey = widgets.NewGlobalKey[*widgets.EditableTextState]() // a new field starts a new history
	e.setModified(false)
	e.refresh()
}

// ClearEditor empties the editor and marks it as saved.
func (e *Editor) ClearEditor() {
	e.setValue("", 0)
	e.setModified(false)
}

// Modified reports whether the text has changed since it was loaded or saved.
func (e *Editor) Modified() bool { return e.modified }

// SetModified marks the text as changed or as saved, without changing it.
func (e *Editor) SetModified(modified bool) { e.setModified(modified) }

func (e *Editor) setModified(modified bool) {
	if e.modified == modified {
		return
	}
	e.modified = modified
	if e.hooks.ModifiedChanged != nil {
		e.hooks.ModifiedChanged(modified)
	}
}

// CursorPosition returns the caret in UTF-16 units.
func (e *Editor) CursorPosition() int {
	return e.toUnits(max(e.controller.Selection().ExtentOffset, 0))
}

// MoveCursorTo puts the caret at pos (UTF-16 units) and scrolls it into view. It
// does not take the keyboard focus away from the widget that has it.
func (e *Editor) MoveCursorTo(pos int) {
	b := e.toBytes(pos)
	e.controller.SetSelection(services.NewTextSelectionCollapsed(b))
	e.bringIntoView(b)
}

// SelectRange selects the text between the UTF-16 positions start and end.
func (e *Editor) SelectRange(start, end int) {
	e.controller.SetSelection(services.NewTextSelection(e.toBytes(start), e.toBytes(end)))
}

func (e *Editor) bringIntoView(byteOffset int) {
	if st, ok := e.fieldKey.CurrentState(); ok {
		st.BringIntoView(ui.NewTextPosition(byteOffset))
	}
}

// selectedRange returns the selection in UTF-16 units. ok is false when nothing
// is selected or the selection holds only white space, so there is nothing to
// rewrite.
func (e *Editor) selectedRange() (start, end int, ok bool) {
	sel := e.controller.Selection()
	if sel.IsCollapsed() || sel.BaseOffset < 0 {
		return 0, 0, false
	}
	a, b := min(sel.BaseOffset, sel.ExtentOffset), max(sel.BaseOffset, sel.ExtentOffset)
	b = min(b, len(e.text))
	if strings.TrimSpace(e.text[a:b]) == "" {
		return 0, 0, false
	}
	return e.toUnits(a), e.toUnits(b), true
}

// ReplaceRange replaces the text between the UTF-16 positions start and end with
// text, and puts the caret after it. It is reported like typing.
func (e *Editor) ReplaceRange(start, end int, text string) {
	a, b := e.toBytes(start), e.toBytes(end)
	old := e.controller.Text()
	e.setValue(old[:a]+text+old[b:], a+len(text))
	e.bringIntoView(a + len(text))
}

// InsertTextAt inserts text at the UTF-16 position pos, as if it were typed.
func (e *Editor) InsertTextAt(pos int, text string) { e.ReplaceRange(pos, pos, text) }

// TypeIntoEditor types text at the end of the editor, rune by rune, as the user
// would; a read-only editor ignores it.
func (e *Editor) TypeIntoEditor(text string) {
	if e.busy {
		return
	}
	for _, r := range text {
		cur := e.controller.Text()
		e.setValue(cur+string(r), len(cur)+utf8.RuneLen(r))
	}
}

// Undo undoes the last edit.
func (e *Editor) Undo() { e.undo.Undo() }

// Focus gives the keyboard focus to the editor.
func (e *Editor) Focus() { e.focus.RequestFocus() }

// SetFont sets the font family and its size in points. An empty family is the
// theme's; a size below 1 is ignored.
func (e *Editor) SetFont(f store.Font) {
	if f.Size < 1 {
		return
	}
	e.font = f
	e.refresh()
}

// Font returns the font family and its size in points.
func (e *Editor) Font() store.Font { return e.font }

// SetBusy puts the loading screen over the editor, or takes it away. While it is
// up the text cannot be edited, because the result of the check in progress is
// for the text as it is.
func (e *Editor) SetBusy(busy bool) {
	e.busy = busy
	e.busyHint = false
	e.refresh()
}

// Busy reports whether the loading screen is up.
func (e *Editor) Busy() bool { return e.busy }

// ShowBusyHint shows the line under the busy indicator that says the user will
// be notified.
func (e *Editor) ShowBusyHint() {
	e.busyHint = e.busy
	e.refresh()
}

// BusyHintShown reports whether that line is shown.
func (e *Editor) BusyHintShown() bool { return e.busy && e.busyHint }

// SetUnderlines replaces the underlines. They are only drawn, so they never
// change the text or the undo history.
func (e *Editor) SetUnderlines(us []Underline) {
	e.underlines = append([]Underline(nil), us...)
	e.refresh()
	if st, ok := e.fieldKey.CurrentState(); ok {
		if r := st.RenderEditable(); r != nil {
			r.MarkNeedsPaint()
		}
	}
}

// Underlines returns the underlines last set.
func (e *Editor) Underlines() []Underline { return append([]Underline(nil), e.underlines...) }

// ---- Drawing ----

// buildSpan builds the text of the field: the plain text with the underlines
// drawn as decorations of ranges. The selected underline comes last so that its
// background wins where underlines overlap.
func (e *Editor) buildSpan(bc widgets.BuildContext, style *painting.TextStyle, _ bool) *painting.TextSpan {
	text := e.controller.Text()
	if len(e.underlines) == 0 || text == "" {
		return painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(text), Style: style})
	}
	dark := isDark(bc)
	umap := analysis.NewUTF16Map(text)
	type placed struct {
		start, end int
		u          Underline
	}
	var list []placed
	for _, pass := range []bool{false, true} {
		for _, u := range e.underlines {
			if u.Selected != pass {
				continue
			}
			s, t := umap.ByteOffset(u.Start), umap.ByteOffset(u.End)
			if t > s {
				list = append(list, placed{s, t, u})
			}
		}
	}
	cuts := []int{0, len(text)}
	for _, p := range list {
		cuts = append(cuts, p.start, p.end)
	}
	sort.Ints(cuts)
	var children []painting.InlineSpan
	for i := 0; i+1 < len(cuts); i++ {
		a, b := cuts[i], cuts[i+1]
		if a == b {
			continue
		}
		var top *Underline
		for j := range list {
			if list[j].start <= a && b <= list[j].end {
				top = &list[j].u
			}
		}
		o := painting.TextSpanOpts{Text: foundation.Some(text[a:b])}
		if top != nil {
			o.Style = underlineStyle(*top, dark)
		}
		children = append(children, painting.NewTextSpan(o))
	}
	return painting.NewTextSpan(painting.TextSpanOpts{Style: style, Children: children})
}

// underlineStyle is the look of an underlined range.
func underlineStyle(u Underline, dark bool) *painting.TextStyle {
	color := hexColor(view.CategoryColor(u.Category, dark))
	o := painting.TextStyleOpts{
		Decoration:      foundation.Some(ui.TextDecorationUnderline),
		DecorationColor: foundation.Some(color),
	}
	switch view.CategoryStyle(u.Category) {
	case view.UnderlineDash:
		o.DecorationStyle = ui.TextDecorationStyleDashed
	case view.UnderlineDot:
		o.DecorationStyle = ui.TextDecorationStyleDotted
	default:
		o.DecorationStyle = ui.TextDecorationStyleWavy
	}
	if u.Selected {
		o.BackgroundColor = foundation.Some(color.WithAlpha(selectedAlpha))
	}
	return painting.NewTextStyle(o)
}

// ---- The widget ----

// Widget is the editor as a widget. It fills the space it is given.
func (e *Editor) Widget() widgets.Widget {
	return m.NewListenableBuilder(e.changes, func(bc widgets.BuildContext, _ widgets.Widget) widgets.Widget {
		return e.build(bc)
	}, nil)
}

func (e *Editor) build(bc widgets.BuildContext) widgets.Widget {
	theme := m.ThemeOf(bc)
	base := theme.TextTheme.BodyLarge.CopyWith(painting.TextStyleCopy{FontFamily: e.font.Family, FontSize: foundation.Some(pixels(e.font.Size))})
	field := m.NewEditableText(e.controller, e.focus, base, theme.ColorScheme.Primary(), theme.ColorScheme.OnSurface(),
		widgets.EditableTextOpts{
			Key:                    e.fieldKey,
			Autofocus:              true,
			ReadOnly:               e.busy,
			Expands:                true,
			MaxLines:               foundation.Some(rendering.MaxLinesUnlimited),
			RendererIgnoresPointer: true,
			SelectionColor:         foundation.Some(theme.ColorScheme.Primary().WithAlpha(80)),
			SelectionControls:      widgets.NewEmptyTextSelectionControls(),
			KeyboardType:           foundation.Some(services.TextInputTypeMultiline),
			TextInputAction:        services.TextInputActionNewline,
			MouseCursor:            services.SystemMouseCursors.Text,
			UndoController:         e.undo,
			ContextMenuBuilder:     e.contextMenu,
		})
	body := e.gestures.BuildGestureDetector(field, widgets.TextSelectionGestureDetectorBuildOpts{
		Behavior: rendering.HitTestBehaviorTranslucent,
	})
	layers := []widgets.Widget{m.NewPadding(painting.NewEdgeInsetsAll(8), body)}
	if e.controller.Text() == "" {
		layers = append(layers, m.NewIgnorePointer(m.NewPadding(painting.NewEdgeInsetsAll(8),
			m.NewText(e.placeholt, m.TextOpts{Style: base.CopyWith(painting.TextStyleCopy{Color: foundation.Some(theme.ColorScheme.OnSurfaceVariant())})}))))
	}
	if e.busy {
		layers = append(layers, e.busyCover(bc))
	}
	if e.hoverText != "" && !e.busy {
		layers = append(layers, m.NewPositioned(e.tooltip(bc), m.PositionedOpts{
			Left: foundation.Some(e.hoverAt.Dx + 12), Top: foundation.Some(e.hoverAt.Dy + 22),
		}))
	}
	return m.NewMouseRegion(m.NewStack(layers, m.StackOpts{Fit: rendering.StackFitExpand, ClipBehavior: ui.ClipHardEdge}), widgets.MouseRegionOpts{
		OnHover: e.onHover,
		OnExit:  func(*gestures.PointerExitEvent) { e.setHover("", ui.Offset{}) },
	})
}

// busyText is the text of the cover while a check runs.
const busyText = "Checking..."

// busyHintText is shown under the busy indicator when a check takes long.
const busyHintText = "We will notify you when the results are ready."

// busyCover is the loading screen: a translucent layer over the text with a
// label and a busy indicator. It takes the clicks, so they do not reach the text.
func (e *Editor) busyCover(bc widgets.BuildContext) widgets.Widget {
	items := []widgets.Widget{
		m.NewText(busyText, m.TextOpts{Style: m.ThemeOf(bc).TextTheme.TitleMedium}),
		m.NewSizedBox(nil, m.SizedBoxOpts{Height: foundation.Some(8.0)}),
		m.NewSizedBox(m.NewLinearProgressIndicator(), m.SizedBoxOpts{Width: foundation.Some(240.0)}),
	}
	if e.busyHint {
		items = append(items, m.NewSizedBox(nil, m.SizedBoxOpts{Height: foundation.Some(8.0)}),
			m.NewText(busyHintText))
	}
	return m.NewAbsorbPointer(m.NewColoredBox(ui.NewColor(0x6E7F7F7F), m.NewCenter(
		m.NewColumn(items, m.ColumnOpts{MainAxisSize: m.MainAxisSizeMin}))))
}

// tooltip is the box that shows the text of the issue under the mouse.
func (e *Editor) tooltip(bc widgets.BuildContext) widgets.Widget {
	theme := m.ThemeOf(bc)
	return m.NewIgnorePointer(m.NewConstrainedBox(
		rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(360.0)}),
		m.NewContainer(m.NewText(e.hoverText, m.TextOpts{Style: theme.TextTheme.BodySmall.CopyWith(
			painting.TextStyleCopy{Color: foundation.Some(theme.ColorScheme.OnInverseSurface())})}),
			m.ContainerOpts{
				Padding: painting.NewEdgeInsetsSymmetric(6, 10),
				Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
					Color:        foundation.Some(theme.ColorScheme.InverseSurface()),
					BorderRadius: painting.NewBorderRadiusAll(ui.NewRadiusCircular(4)),
				}),
			})))
}

func (e *Editor) onHover(ev *gestures.PointerHoverEvent) {
	if e.hooks.TooltipAt == nil || e.busy {
		return
	}
	st, ok := e.fieldKey.CurrentState()
	if !ok || st.RenderEditable() == nil {
		return
	}
	pos := e.toUnits(st.RenderEditable().GetPositionForPoint(ev.Position()).Offset)
	e.setHover(e.hooks.TooltipAt(pos), ev.LocalPosition())
}

func (e *Editor) setHover(text string, at ui.Offset) {
	if text == e.hoverText && (text == "" || at == e.hoverAt) {
		return
	}
	e.hoverText, e.hoverAt = text, at
	e.refresh()
}

// HoverText returns the tooltip that is shown, or "".
func (e *Editor) HoverText() string { return e.hoverText }

// ---- The right-click menu ----

// menuItems are the entries of the right-click menu: the standard ones of the
// field, and Alternative and Check when text is selected. The selection is read
// when an entry is chosen, so it is the one the user saw. Check is disabled while
// no LLM is configured, like the Check button.
func (e *Editor) menuItems(std []widgets.ContextMenuButtonItem) []widgets.ContextMenuButtonItem {
	items := append([]widgets.ContextMenuButtonItem(nil), std...)
	if _, _, ok := e.selectedRange(); !ok {
		return items
	}
	items = append(items, widgets.NewContextMenuButtonItem(func() {
		if s, t, ok := e.selectedRange(); ok && e.hooks.Alternative != nil {
			e.hooks.Alternative(s, t)
		}
	}, widgets.ContextMenuButtonItemOpts{Label: foundation.Some(alternativeLabel)}))
	var onCheck foundation.VoidCallback
	if e.hooks.CheckEnabled == nil || e.hooks.CheckEnabled() {
		onCheck = func() {
			if s, t, ok := e.selectedRange(); ok && e.hooks.CheckSelection != nil {
				e.hooks.CheckSelection(s, t)
			}
		}
	}
	return append(items, widgets.NewContextMenuButtonItem(onCheck, widgets.ContextMenuButtonItemOpts{Label: foundation.Some(checkLabel)}))
}

func (e *Editor) contextMenu(_ widgets.BuildContext, st *widgets.EditableTextState) widgets.Widget {
	if e.busy {
		return m.NewSizedBox(nil) // the loading screen is up
	}
	return m.NewAdaptiveTextSelectionToolbarButtonItems(e.menuItems(st.ContextMenuButtonItems()), st.ContextMenuAnchors())
}

// MenuItems returns the entries of the right-click menu as it is now, for tests
// and the documentation's screenshots.
func (e *Editor) MenuItems() []widgets.ContextMenuButtonItem {
	var std []widgets.ContextMenuButtonItem
	if st, ok := e.fieldKey.CurrentState(); ok {
		std = st.ContextMenuButtonItems()
	}
	return e.menuItems(std)
}

// FieldState returns the state of the text field, for the documentation's
// pictures and tests; it is false while the editor is not on screen.
func (e *Editor) FieldState() (*widgets.EditableTextState, bool) { return e.fieldKey.CurrentState() }

// hexColor reads a colour written as #RRGGBB.
func hexColor(s string) ui.Color {
	var v uint32
	for _, c := range strings.TrimPrefix(s, "#") {
		v <<= 4
		switch {
		case c >= '0' && c <= '9':
			v |= uint32(c - '0')
		case c >= 'a' && c <= 'f':
			v |= uint32(c-'a') + 10
		case c >= 'A' && c <= 'F':
			v |= uint32(c-'A') + 10
		}
	}
	return ui.NewColor(0xFF000000 | v)
}

// isDark reports whether the theme is dark.
func isDark(bc widgets.BuildContext) bool {
	return m.ThemeOf(bc).ColorScheme.Brightness() == ui.BrightnessDark
}
