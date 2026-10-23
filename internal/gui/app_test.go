package gui

import (
	"slices"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/store"
)

func TestTrayMenu(t *testing.T) {
	var got []string
	rec := func(s string) func() { return func() { got = append(got, s) } }
	tray := NewTray(TrayActions{Open: rec("open"), Quit: rec("quit"), ToggleWeb: rec("web"), OpenWeb: rec("page")})
	if want := []string{TrayOpenLabel, webStartText, TrayCloseLabel}; !slices.Equal(tray.MenuLabels(), want) {
		t.Errorf("labels = %v, want %v", tray.MenuLabels(), want)
	}
	tray.SetWebRunning(true)
	if want := []string{TrayOpenLabel, webStopText, TrayOpenWebLabel, TrayCloseLabel}; !slices.Equal(tray.MenuLabels(), want) {
		t.Errorf("labels while the server runs = %v, want %v", tray.MenuLabels(), want)
	}
	for _, label := range []string{TrayOpenLabel, webStopText, TrayOpenWebLabel, TrayCloseLabel} {
		if !tray.Choose(label) {
			t.Errorf("no item %q", label)
		}
	}
	if tray.Choose("nothing") || !slices.Equal(got, []string{"open", "web", "page", "quit"}) {
		t.Errorf("reports %v", got)
	}
	bare := NewTray(TrayActions{Quit: rec("quit")})
	if !slices.Equal(bare.MenuLabels(), []string{TrayCloseLabel}) {
		t.Errorf("an item without an action is left out: %v", bare.MenuLabels())
	}
}

func TestTrayIsAvailableUntilGlutterSaysOtherwise(t *testing.T) {
	tray := NewTray(TrayActions{})
	gone := 0
	tray.SetUnavailableHandler(func() { gone++ })
	if !tray.Available() {
		t.Fatal("the icon should be drawn: the tray starts available")
	}
	tray.MarkUnavailable()
	tray.MarkUnavailable()
	if tray.Available() || gone != 1 {
		t.Errorf("available %v, handler called %d times", tray.Available(), gone)
	}
}

func TestIsTrayUnavailable(t *testing.T) {
	if !IsTrayUnavailable(errors.New("TrayUnavailable", "no tray")) {
		t.Error("the tag TrayUnavailable is the report of a missing tray")
	}
	if IsTrayUnavailable(errors.New("Other", "x")) || IsTrayUnavailable(nil) {
		t.Error("other errors are not")
	}
}

func TestRootGivesTheButtonsTheirFont(t *testing.T) {
	tests := []struct {
		name   string
		font   store.Font
		family string
		size   float64
	}{
		{"default", store.DefaultFonts().Buttons, "", pixels(store.DefaultButtonFontSize)},
		{"chosen", store.Font{Family: "Test Sans", Size: 15}, "Test Sans", 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tester := gluttertest.New(t)
			rt := NewRuntime(nil)
			w := NewMainWindow(rt)
			t.Cleanup(w.Dispose)
			tester.PumpWidget(Root(w, nil, rt))
			f := w.Fonts()
			f.Buttons = tt.font
			w.SetFonts(f)
			tester.PumpAndSettle()
			label := m.ThemeOf(tester.Element(gluttertest.ByText("Preferences"))).TextTheme.LabelLarge
			if (tt.family != "" && label.FontFamily != tt.family) || label.FontSize.OrElse(0) != tt.size {
				t.Errorf("label style %q %v, want %q %v", label.FontFamily, label.FontSize.OrElse(0), tt.family, tt.size)
			}
		})
	}
}

func TestRootShowsTheWindowAndHandlesTheCloseButton(t *testing.T) {
	tester := gluttertest.New(t)
	rt := NewRuntime(nil)
	w := NewMainWindow(rt)
	t.Cleanup(w.Dispose)
	var requested int
	w.SetHandlers(Handlers{CloseRequested: func() { requested++ }})
	tester.PumpWidget(Root(w, nil, rt))
	if rt.Binding() == nil {
		t.Fatal("the runtime should be attached when the app first builds")
	}
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("Preferences")); err != nil {
		t.Error(err)
	}
	if resp := tester.Binding.ExitApplication(ui.AppExitTypeCancelable); resp != ui.AppExitResponseCancel {
		t.Errorf("the window must not close by itself: the application decides, got %v", resp)
	}
	if requested != 1 {
		t.Errorf("requested %d", requested)
	}
	w.Exit()
	tester.Pump()
}

func TestRuntimeQueuesWorkUntilItIsAttached(t *testing.T) {
	tester := gluttertest.New(t)
	tester.PumpWidget(m.NewSizedBox(nil))
	rt := NewRuntime(nil)
	var order []string
	rt.Post(func() { order = append(order, "post") })
	stop := rt.After(0, func() { order = append(order, "never") })
	stop()
	rt.After(0, func() { order = append(order, "after") })
	rt.AfterFrame(func() { order = append(order, "frame") })
	if len(order) != 0 {
		t.Fatalf("work ran before the runtime was attached: %v", order)
	}
	rt.Attach(tester.Binding)
	rt.Attach(tester.Binding) // a second attach changes nothing
	tester.FlushPosted()
	tester.Pump(time.Second)
	tester.Pump(time.Second)
	if !slices.Contains(order, "post") || !slices.Contains(order, "after") || !slices.Contains(order, "frame") || slices.Contains(order, "never") {
		t.Errorf("order = %v", order)
	}
}

func TestCategoryColoursReachTheUnderlines(t *testing.T) {
	// The three categories look different from each other in the editor.
	seen := map[string]bool{}
	for _, c := range legendOrder {
		s := underlineStyle(Underline{Category: c}, false)
		key := s.DecorationStyle.Name() + s.DecorationColor.OrElse(ui.NewColor(0)).String()
		if seen[key] {
			t.Errorf("category %v looks like another one", c)
		}
		seen[key] = true
	}
	_ = analysis.Grammar
}
