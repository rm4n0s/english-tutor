package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/english-tutor/internal/uitest"
)

type altEvents struct {
	retries, closed int
	copied, chosen  []string
}

func withAlternatives(t *testing.T, selected string, f func(d *AlternativesDialog, ev *altEvents)) {
	t.Helper()
	uitest.OnUI(func() {
		ev := &altEvents{}
		d := NewAlternativesDialog(nil, selected, AlternativesHooks{
			Retry:  func() { ev.retries++ },
			Chosen: func(text string) { ev.chosen = append(ev.chosen, text) },
			Closed: func() { ev.closed++ },
		})
		defer d.dlg.Delete()
		d.SetCopyAction(func(text string) { ev.copied = append(ev.copied, text) })
		f(d, ev)
	})
}

func TestAlternativesDialogStartsLoading(t *testing.T) {
	withAlternatives(t, "a very big dog", func(d *AlternativesDialog, _ *altEvents) {
		if !d.Loading() || d.StatusText() != "Looking for alternatives..." {
			t.Errorf("loading = %v, status %q", d.Loading(), d.StatusText())
		}
		if d.SelectionText() != "a very big dog" {
			t.Errorf("selection = %q", d.SelectionText())
		}
		if d.CopyEnabled() || d.RetryVisible() || d.ErrorText() != "" || len(d.Alternatives()) != 0 {
			t.Error("nothing to copy, retry or show while loading")
		}
	})
}

func TestAlternativesDialogShowsTheList(t *testing.T) {
	withAlternatives(t, "big", func(d *AlternativesDialog, _ *altEvents) {
		alts := []string{"huge", "large", "a rather big one"}
		d.SetAlternatives(alts)
		if d.Loading() || d.StatusText() != "3 alternatives" {
			t.Errorf("loading = %v, status %q", d.Loading(), d.StatusText())
		}
		if !slices.Equal(d.Alternatives(), alts) || d.list.Count() != 3 || d.list.Item(2).Text() != "a rather big one" {
			t.Errorf("rows: %d, alternatives %q", d.list.Count(), d.Alternatives())
		}
		if d.list.CurrentRow() != 0 || !d.CopyEnabled() || d.RetryVisible() {
			t.Error("the first row should be selected, Copy enabled and Retry hidden")
		}
	})
}

func TestAlternativesDialogStatusWording(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{{0, "No alternatives found."}, {1, "1 alternative"}, {2, "2 alternatives"}, {8, "8 alternatives"}}
	for _, tt := range tests {
		if got := AlternativesStatus(tt.n); got != tt.want {
			t.Errorf("AlternativesStatus(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
	withAlternatives(t, "big", func(d *AlternativesDialog, _ *altEvents) {
		d.SetAlternatives(nil)
		if d.StatusText() != "No alternatives found." || d.CopyEnabled() {
			t.Errorf("status %q, copy enabled %v", d.StatusText(), d.CopyEnabled())
		}
	})
}

func TestAlternativesDialogCopy(t *testing.T) {
	withAlternatives(t, "big", func(d *AlternativesDialog, ev *altEvents) {
		d.SetAlternatives([]string{"huge", "line one\nline two", "large"})
		d.CopyClick()
		d.SelectRow(1)
		d.CopyClick()
		d.SelectRow(2)
		d.CopyClick()
		want := []string{"huge", "line one\nline two", "large"}
		if !slices.Equal(ev.copied, want) {
			t.Errorf("copied %q, want %q", ev.copied, want)
		}
		if len(ev.chosen) != 0 {
			t.Errorf("Copy must not choose: %q", ev.chosen)
		}
	})
}

func TestAlternativesDialogDoubleClickChoosesTheRow(t *testing.T) {
	withAlternatives(t, "big", func(d *AlternativesDialog, ev *altEvents) {
		d.SetAlternatives([]string{"huge", "line one\nline two", "large"})
		d.DoubleClickRow(1)
		d.DoubleClickRow(2)
		d.DoubleClickRow(7) // no such row
		if want := []string{"line one\nline two", "large"}; !slices.Equal(ev.chosen, want) {
			t.Errorf("chosen %q, want %q", ev.chosen, want)
		}
		if len(ev.copied) != 0 {
			t.Errorf("a double click must not copy: %q", ev.copied)
		}
	})
}

func TestAlternativesDialogNoticeKeepsTheList(t *testing.T) {
	withAlternatives(t, "big", func(d *AlternativesDialog, _ *altEvents) {
		d.SetAlternatives([]string{"huge", "large"})
		d.SetNotice("The text has changed")
		if d.ErrorText() != "The text has changed" || len(d.Alternatives()) != 2 || !d.CopyEnabled() {
			t.Errorf("error %q, rows %d, copy %v", d.ErrorText(), len(d.Alternatives()), d.CopyEnabled())
		}
		if d.RetryVisible() {
			t.Error("a notice is not a failure: nothing to retry")
		}
		d.SelectRow(1)
		if d.ErrorText() == "" {
			t.Error("the notice should stay until the next request")
		}
		d.SetLoading()
		if d.ErrorText() != "" {
			t.Error("a new request clears the notice")
		}
	})
}

func TestAlternativesDialogCopyDoesNothingWithoutARow(t *testing.T) {
	withAlternatives(t, "big", func(d *AlternativesDialog, ev *altEvents) {
		d.copySelected()
		d.SetFailed("boom")
		d.copySelected()
		if len(ev.copied) != 0 {
			t.Errorf("copied %q with nothing selected", ev.copied)
		}
	})
}

func TestAlternativesDialogFailureAndRetry(t *testing.T) {
	withAlternatives(t, "big", func(d *AlternativesDialog, ev *altEvents) {
		d.SetAlternatives([]string{"huge"})
		d.SetFailed("The request to local failed: connection refused")
		if d.Loading() || d.ErrorText() != "The request to local failed: connection refused" || d.StatusText() != "Could not get alternatives." {
			t.Errorf("loading %v, error %q, status %q", d.Loading(), d.ErrorText(), d.StatusText())
		}
		if len(d.Alternatives()) != 0 || d.CopyEnabled() || !d.RetryVisible() {
			t.Error("a failure clears the list and offers Retry")
		}
		d.RetryClick()
		if ev.retries != 1 {
			t.Errorf("retries = %d", ev.retries)
		}
		d.SetLoading()
		if !d.Loading() || d.ErrorText() != "" || d.RetryVisible() {
			t.Error("loading again clears the failure")
		}
		d.SetAlternatives([]string{"large"})
		if d.ErrorText() != "" || d.RetryVisible() || d.StatusText() != "1 alternative" {
			t.Errorf("error %q, retry %v, status %q", d.ErrorText(), d.RetryVisible(), d.StatusText())
		}
	})
}

func TestAlternativesDialogClosedHookRunsOnce(t *testing.T) {
	withAlternatives(t, "big", func(d *AlternativesDialog, ev *altEvents) {
		d.Show()
		if !d.IsVisible() {
			t.Fatal("the dialog should be shown")
		}
		d.Close()
		if d.IsVisible() || ev.closed != 1 {
			t.Errorf("visible %v, closed hook %d times", d.IsVisible(), ev.closed)
		}
		d.onClosed()
		if ev.closed != 1 {
			t.Errorf("closed hook ran %d times", ev.closed)
		}
	})
}

func TestShortSelection(t *testing.T) {
	long := strings.Repeat("é", maxShownSelection+50)
	tests := []struct {
		name, in, want string
	}{
		{"short", "a big dog", "a big dog"},
		{"exactly the limit", strings.Repeat("x", maxShownSelection), strings.Repeat("x", maxShownSelection)},
		{"cut by characters, not bytes", long, strings.Repeat("é", maxShownSelection) + "…"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		if got := ShortSelection(tt.in); got != tt.want {
			t.Errorf("%s: got %d bytes, want %d", tt.name, len(got), len(tt.want))
		}
	}
}

func TestAlternativesDialogShowsLongSelectionCut(t *testing.T) {
	long := strings.Repeat("word ", 200)
	withAlternatives(t, long, func(d *AlternativesDialog, _ *altEvents) {
		if got := d.SelectionText(); !strings.HasSuffix(got, "…") || len([]rune(got)) != maxShownSelection+1 {
			t.Errorf("selection shows %d characters", len([]rune(got)))
		}
	})
}
