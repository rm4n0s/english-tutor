package web

import (
	"context"
	"crypto/rand"
	"fmt"
	"github.com/rm4n0s/english-tutor/internal/view"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/issues"
	"github.com/rm4n0s/english-tutor/internal/llm"
	"github.com/rm4n0s/english-tutor/internal/logging"
	"github.com/rm4n0s/english-tutor/internal/store"
)

// maxTextBytes is the longest text the page works with. A text longer than this
// is not an essay (the desktop window opens at most 10 MB files too).
const maxTextBytes = 10 << 20

// regionSet is a set of parts of the page, each one an element with an id.
type regionSet uint32

// The parts of the page that are pushed separately.
const (
	rBanner regionSet = 1 << iota
	rUnder            // the layer under the editor that draws the underlines
	rIssues
	rExplain
	rStatus
	rToolbar
	rModal
	rSignals  // the small signals: title, modified flag, font size
	rText     // the text of the editor itself; only after New, Open and the like
	rDownload // a file for the browser to save
	rAll      = rBanner | rUnder | rIssues | rExplain | rStatus | rToolbar | rModal | rSignals | rText
)

// workspace is the state of one open page: what the desktop window and the App
// hold for the one window. Its fields belong to mu.
type workspace struct {
	id    string
	owner string // session token of the browser that made it
	run   *run
	srv   *Server

	mu     sync.Mutex
	closed bool

	text      string
	savedText string
	state     *issues.State
	analyzer  *analysis.Analyzer
	explainer *analysis.Explainer

	llms      []llmChoice
	llmID     uint
	autoCheck bool
	autoDelay time.Duration
	autoTimer *time.Timer
	fontSize  int

	sourceKey  string
	place      place
	lineEnding string

	checkSeq     uint64
	checking     bool
	status       view.CheckStatus
	unplaced     int
	lastUsage    llm.Usage
	sessionUsage llm.Usage
	notice       string // replaces the status for a moment ("Nothing to check yet")
	banner       string

	resultRecorded bool
	typeCounts     map[string]int
	lastCheckID    uint64
	fromEditor     bool // the selection came from the text cursor, which must not move
	gotoSeq        int  // number of the last "put the cursor here" request
	gotoPos        int

	explainSeq    uint64
	explainState  view.ExplainState
	explainHeader string
	explainBody   string // the markdown being shown
	explainBuf    strings.Builder
	explainTitle  string // the issue's snippet, for the loading line

	modal    *modal
	download *pendingDownload

	subs     map[*subscriber]struct{}
	lastSeen time.Time
}

type llmChoice struct {
	ID   uint
	Name string
}

// place says where the text came from or was last saved to (section 7).
type place struct {
	docID    uint // library document the text is tied to, or 0
	docTitle string
	name     string // what the title shows; "" for an unsaved text
}

const (
	untitled  = "Untitled"
	titleRune = 60
	appTitle  = "English Tutor"
)

// pendingDownload is a file the browser fetches once.
type pendingDownload struct {
	token   string
	name    string
	content []byte
	expires time.Time
}

// newWorkspace makes the state of a new page. The settings are read once; the
// page then follows its own state.
func (s *Server) newWorkspace(r *run, owner string) *workspace {
	w := &workspace{
		id:         randomToken()[:22],
		owner:      owner,
		run:        r,
		srv:        s,
		state:      issues.New(),
		analyzer:   analysis.NewAnalyzer(analysis.AnalyzerConfig{Usage: s.deps.Stats, Logger: s.deps.Log}),
		explainer:  analysis.NewExplainer(analysis.ExplainerConfig{Usage: s.deps.Stats, Logger: s.deps.Log}),
		sourceKey:  newSessionKey(),
		lineEnding: "\n",
		typeCounts: map[string]int{},
		subs:       map[*subscriber]struct{}{},
		autoDelay:  store.DefaultAutoCheckSec * time.Second,
		fontSize:   store.DefaultFontSize,
		lastSeen:   s.now(),
	}
	w.state.Subscribe(w.onStateChange)
	w.loadSettings()
	w.reloadLLMs()
	w.refreshCounts()
	return w
}

func (w *workspace) ctx() context.Context { return w.run.ctx }

func (w *workspace) logError(msg string, err error) { logging.LogError(w.srv.deps.Log, msg, err) }

// loadSettings reads the settings every page starts from. A setting that cannot
// be read is logged and replaced by its default.
func (w *workspace) loadSettings() {
	st := w.srv.deps.Store
	if on, err := st.AutoCheck(w.ctx()); err != nil {
		w.logError("reading auto-check setting", err)
	} else {
		w.autoCheck = on
	}
	if secs, err := st.AutoCheckDelaySeconds(w.ctx()); err != nil {
		w.logError("reading auto-check delay", err)
	} else {
		w.autoDelay = time.Duration(secs) * time.Second
	}
	if size, err := st.FontSize(w.ctx()); err != nil {
		w.logError("reading font size", err)
	} else {
		w.fontSize = size
	}
}

// reloadLLMs reads the list of LLMs and keeps the selection when the LLM still
// exists. The remembered selection of the desktop window is the starting point.
func (w *workspace) reloadLLMs() {
	st := w.srv.deps.Store
	cfgs, err := st.ListLLMConfigs(w.ctx())
	if err != nil {
		w.logError("reading LLM list", err)
	}
	w.llms = make([]llmChoice, len(cfgs))
	for i, c := range cfgs {
		w.llms[i] = llmChoice{ID: c.ID, Name: c.Name}
	}
	if w.llmID == 0 {
		if id, err := st.SelectedLLMID(w.ctx()); err != nil {
			w.logError("reading selected LLM", err)
		} else {
			w.llmID = id
		}
	}
	for _, c := range w.llms {
		if c.ID == w.llmID {
			return
		}
	}
	w.llmID = 0
	if len(w.llms) > 0 {
		w.llmID = w.llms[0].ID
	}
}

// refreshCounts reads the all-time count of every error type, for the badges of
// the issues list.
func (w *workspace) refreshCounts() {
	rows, err := w.srv.deps.Stats.TypeStats(w.ctx())
	if err != nil {
		w.logError("reading statistics", err)
		return
	}
	w.typeCounts = make(map[string]int, len(rows))
	for _, r := range rows {
		w.typeCounts[r.ErrorType] = r.Total
	}
}

// refreshCountsLater does the same without holding up the caller.
func (w *workspace) refreshCountsLater() {
	w.run.spawn(func() {
		rows, err := w.srv.deps.Stats.TypeStats(w.ctx())
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.closed {
			return
		}
		if err != nil {
			w.logError("reading statistics", err)
			return
		}
		w.typeCounts = make(map[string]int, len(rows))
		for _, r := range rows {
			w.typeCounts[r.ErrorType] = r.Total
		}
		w.touch(rIssues)
	})
}

// close ends what the page started. The state is not used afterwards.
func (w *workspace) close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.closed = true
	w.analyzer.Cancel()
	w.explainer.Cancel()
	if w.autoTimer != nil {
		w.autoTimer.Stop()
	}
	for s := range w.subs {
		s.stop()
	}
}

// idleSince is how long ago the page was last used. A page with a browser
// connected is never idle.
func (w *workspace) idleSince(now time.Time) time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.subs) > 0 {
		return 0
	}
	return now.Sub(w.lastSeen)
}

func (w *workspace) connected() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.subs) > 0
}

// --- subscribers: the browsers connected to the event stream ---

// subscriber is one event stream. dirty is what it has not been sent yet.
type subscriber struct {
	wake  chan struct{}
	dirty regionSet
	done  chan struct{}
	once  sync.Once
}

func (s *subscriber) stop() { s.once.Do(func() { close(s.done) }) }

// subscribe adds a stream. It starts with everything dirty but the text of the
// editor, so a browser that connects (or reconnects) gets the whole page and
// keeps what it has in its editor, which may be newer than the server's copy.
func (w *workspace) subscribe() *subscriber {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := &subscriber{wake: make(chan struct{}, 1), dirty: rAll &^ rText, done: make(chan struct{})}
	if w.closed {
		s.stop()
	}
	w.subs[s] = struct{}{}
	w.lastSeen = w.srv.now()
	w.reloadLLMs() // the list may have been changed in the desktop window
	s.wake <- struct{}{}
	return s
}

func (w *workspace) unsubscribe(s *subscriber) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.subs, s)
	w.lastSeen = w.srv.now()
}

// touch marks parts of the page as changed, for every connected browser. The
// caller holds mu.
func (w *workspace) touch(r regionSet) {
	for s := range w.subs {
		s.dirty |= r
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
}

// take returns and clears what is dirty for a stream.
func (w *workspace) take(s *subscriber) regionSet {
	w.mu.Lock()
	defer w.mu.Unlock()
	r := s.dirty
	s.dirty = 0
	return r
}

// --- the text ---

// syncText makes the text the browser holds the text of the page, telling the
// issue state about the edit so that underlines follow the words they were on.
func (w *workspace) syncText(newText string) {
	if newText == w.text {
		return
	}
	if len(newText) > maxTextBytes {
		w.setBanner("The text is longer than 10 MB. It is not checked or saved beyond what was sent before.")
		return
	}
	edit := diffEdit(w.text, newText)
	w.text = newText
	w.state.ApplyEdit(edit)
	w.touch(rUnder | rStatus | rSignals)
	w.restartAutoCheck()
}

// diffEdit describes the change from a to b as one replaced range, in UTF-16
// units, the unit the issue state uses. The browser reports the text, not the
// edit, so the edit is found as the part between the common start and end.
func diffEdit(a, b string) issues.Edit {
	ua, ub := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	p := 0
	for p < len(ua) && p < len(ub) && ua[p] == ub[p] {
		p++
	}
	s := 0
	for s < len(ua)-p && s < len(ub)-p && ua[len(ua)-1-s] == ub[len(ub)-1-s] {
		s++
	}
	return issues.Edit{Pos: p, Removed: len(ua) - p - s, Added: len(ub) - p - s}
}

func (w *workspace) modified() bool { return w.text != w.savedText }

// loadText replaces the text with one that was just created or loaded: a
// running check or explanation stops, the issues and the cached results of the
// old text go, and the text counts as saved (section 7).
func (w *workspace) loadText(text string) {
	w.analyzer.Cancel()
	w.explainer.Cancel()
	w.checkSeq++ // the result of a check that was running is stale now
	w.explainSeq++
	w.checking = false
	w.status = view.StatusIdle
	w.unplaced = 0
	w.resultRecorded = false
	w.notice = ""
	w.banner = ""
	w.state.Reset()
	w.text, w.savedText = text, text
	w.explainState = view.ExplainIdle
	w.explainBody, w.explainHeader = "", ""
	w.stopAutoCheck() // loading a text is not typing
	w.touch(rAll)
}

func (w *workspace) displayName() string {
	if w.place.name != "" {
		return w.place.name
	}
	return untitled
}

// windowTitle is the title of the page: the name of the text and a * when it has
// unsaved changes.
func windowTitle(name string, modified bool) string {
	if modified {
		name += "*"
	}
	return name + " — " + appTitle
}

func (w *workspace) setBanner(msg string) {
	w.banner = msg
	w.touch(rBanner)
}

// showError tells the user and logs the failure.
func (w *workspace) showError(msg string, err error) {
	w.logError(msg, err)
	w.setBanner(userMessage(err))
}

// userMessage is the text shown for an error: the message of an application
// error is written for the user.
func userMessage(err error) string {
	if appErr, ok := errors.FromError(err); ok {
		return appErr.Message
	}
	return err.Error()
}

// newSessionKey returns the source key of a new, unsaved text:
// "session:<uuid>" (section 5.7).
func newSessionKey() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand.Read never returns an error; a source key must not be
		// guessable, so do not go on.
		panic(err)
	}
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("session:%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
