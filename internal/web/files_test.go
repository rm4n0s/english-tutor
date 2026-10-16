package web

import (
	"bytes"
	"mime"
	"mime/multipart"
	"net/url"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/english-tutor/internal/llm/llmtest"
	"github.com/rm4n0s/english-tutor/internal/store"
)

func assertRoute(t *testing.T, err error, route string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error with route %q, got nil", route)
	}
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), route)
	}
}

func TestDecodeUpload(t *testing.T) {
	tests := []struct {
		name       string
		data       []byte
		wantText   string
		wantEnding string
	}{
		{"plain", []byte("Hello.\nWorld."), "Hello.\nWorld.", "\n"},
		{"empty", nil, "", "\n"},
		{"unicode", []byte("café 😀\nnaïve"), "café 😀\nnaïve", "\n"},
		{"byte order mark is dropped", []byte("\xef\xbb\xbfHello"), "Hello", "\n"},
		{"windows line endings are remembered", []byte("one\r\ntwo\r\n"), "one\ntwo\n", "\r\n"},
		{"mixed endings count as windows", []byte("one\r\ntwo\nthree"), "one\ntwo\nthree", "\r\n"},
		{"old mac line endings", []byte("one\rtwo"), "one\ntwo", "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, ending, err := decodeUpload(tt.data)
			if err != nil || text != tt.wantText || ending != tt.wantEnding {
				t.Errorf("decodeUpload() = %q, %q, %v; want %q, %q", text, ending, err, tt.wantText, tt.wantEnding)
			}
		})
	}
}

func TestDecodeUploadFailures(t *testing.T) {
	tests := []struct {
		name  string
		data  []byte
		route string
		msg   string
	}{
		{"too large", bytes.Repeat([]byte("a"), maxUploadBytes+1), "decodeUpload.FileReadFailed", "too large"},
		{"invalid utf-8", []byte("caf\xe9"), "decodeUpload.FileNotUTF8", "not valid UTF-8"},
		{"binary", []byte("abc\x00def"), "decodeUpload.FileNotUTF8", "does not look like a text file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := decodeUpload(tt.data)
			assertRoute(t, err, tt.route)
			if appErr, _ := errors.FromError(err); !strings.Contains(appErr.Message, tt.msg) {
				t.Errorf("message = %q, want it to contain %q", appErr.Message, tt.msg)
			}
		})
	}
}

func TestSafeFileName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"essay.txt", "essay.txt"},
		{"notes.md", "notes.md"},
		{"my essay", "my essay.txt"},
		{"", "Untitled.txt"},
		{"   ", "Untitled.txt"},
		{"../../etc/passwd", "passwd.txt"},
		{`C:\Users\me\essay.txt`, "essay.txt"},
		{"a/b/c.txt", "c.txt"},
		{`we"ird:name?.txt`, "weirdname.txt"},
		{"..", "Untitled.txt"},
		{"line\nbreak.txt", "linebreak.txt"},
		{"café.txt", "café.txt"},
	}
	for _, tt := range tests {
		if got := safeFileName(tt.in); got != tt.want {
			t.Errorf("safeFileName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestContentDispositionKeepsNonASCIINames(t *testing.T) {
	h := contentDisposition("café 😀.txt")
	typ, params, err := mime.ParseMediaType(h)
	if err != nil || typ != "attachment" || params["filename"] != "café 😀.txt" {
		t.Errorf("contentDisposition = %q -> %q %v %v", h, typ, params, err)
	}
}

func TestDefaultDocumentTitle(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", "Untitled"},
		{"\n\n  \n", "Untitled"},
		{"Hello world\nsecond", "Hello world"},
		{"\n\nFirst real line", "First real line"},
		{strings.Repeat("x", 100), strings.Repeat("x", 59) + "…"},
	}
	for _, tt := range tests {
		if got := defaultDocumentTitle(tt.in); got != tt.want {
			t.Errorf("defaultDocumentTitle(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestOpenShowsTheFileAndStartsANewSource(t *testing.T) {
	e := newEnv(t, llmtest.New(twoIssues()), Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	ws := e.workspaces()[0]
	before := ws.sourceKey

	if code := b.upload(id, "essay.md", []byte("One.\r\nTwo.")); code != 204 {
		t.Fatalf("open = %d", code)
	}
	s.waitSignal(t, "text", "One.\nTwo.")
	s.waitSignal(t, "_title", "essay.md — English Tutor")
	s.waitSignal(t, "_modified", false)
	s.waitRegion(t, "toolbar", `>essay.md</span>`)
	s.waitRegion(t, "status", "2 words")
	ws.with(func() {
		if ws.sourceKey == before || !strings.HasPrefix(ws.sourceKey, "session:") {
			t.Errorf("an opened file starts a new source: %q (was %q)", ws.sourceKey, before)
		}
		if ws.lineEnding != "\r\n" {
			t.Errorf("the line ending is remembered: %q", ws.lineEnding)
		}
	})

	// Saving writes the same line ending back.
	b.post(id, "save", "One.\nTwo.")
	href := s.waitRegion(t, "download", `data-href="download/`)
	token := between(href, `data-href="download/`, `"`)
	status, body := b.get("/w/" + id + "/download/" + token)
	if status != 200 || body != "One.\r\nTwo." {
		t.Errorf("download = %d %q", status, body)
	}
	// The address works once.
	if status, _ := b.get("/w/" + id + "/download/" + token); status != 404 {
		t.Errorf("the second fetch = %d, want 404", status)
	}
}

func between(s, from, to string) string {
	i := strings.Index(s, from)
	if i < 0 {
		return ""
	}
	s = s[i+len(from):]
	j := strings.Index(s, to)
	if j < 0 {
		return s
	}
	return s[:j]
}

func TestOpenRefusesAFileThatIsNotText(t *testing.T) {
	e := newEnv(t, llmtest.New(), Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	b.post(id, "text", "my draft")
	s.waitSignal(t, "_modified", true)

	b.upload(id, "bad.txt", []byte("caf\xe9"))
	s.waitRegion(t, "banner", "not valid UTF-8")
	ws := e.workspaces()[0]
	ws.with(func() {
		if ws.text != "my draft" {
			t.Errorf("a refused file leaves the text alone: %q", ws.text)
		}
	})
}

func TestNewStartsAnEmptyText(t *testing.T) {
	e := newEnv(t, llmtest.New(twoIssues()), Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	b.post(id, "check", sampleText)
	s.waitRegion(t, "issues", "Issues (2)")
	ws := e.workspaces()[0]
	old := ""
	ws.with(func() { old = ws.sourceKey })

	b.post(id, "new", sampleText)
	s.waitSignal(t, "text", "")
	s.waitRegion(t, "issues", "Issues (0)", "Press Check to find errors")
	s.waitRegion(t, "underlay", ">\u200b</div>")
	s.waitSignal(t, "_modified", false)
	ws.with(func() {
		if ws.sourceKey == old {
			t.Error("New starts a new source")
		}
	})
}

func TestSaveAsAsksForANameAndDownloads(t *testing.T) {
	e := newEnv(t, llmtest.New(), Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	b.post(id, "text", "Dear Sam,\nHello.")
	s.waitSignal(t, "_modified", true)

	// A text without a name asks for one, proposing its first line.
	b.post(id, "save", "Dear Sam,\nHello.")
	s.waitRegion(t, "modal", "Save As", `value="Dear Sam,.txt"`)
	b.postForm(id, "modal/submit", url.Values{"value": {""}})
	s.waitRegion(t, "modal", "Enter a file name.")
	b.postForm(id, "modal/submit", url.Values{"value": {"letter"}})
	s.waitRegion(t, "modal", " hidden")
	href := s.waitRegion(t, "download", `data-href="download/`)
	if !strings.Contains(href, `data-name="letter.txt"`) {
		t.Errorf("the name gets an extension: %q", href)
	}
	token := between(href, `data-href="download/`, `"`)
	resp := b.do("GET", "/w/"+id+"/download/"+token, nil, nil)
	body := readBody(t, resp)
	if body != "Dear Sam,\nHello." || !strings.Contains(resp.Header.Get("Content-Disposition"), `filename=letter.txt`) ||
		!strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") {
		t.Errorf("download: %q %v", body, resp.Header)
	}
	s.waitSignal(t, "_modified", false)
	s.waitSignal(t, "_title", "letter.txt — English Tutor")

	// The next Save needs no question.
	b.post(id, "text", "Dear Sam,\nHello again.")
	b.post(id, "save", "Dear Sam,\nHello again.")
	s.waitRegion(t, "download", `data-name="letter.txt"`)
}

func TestSaveAsCanBeCancelled(t *testing.T) {
	e := newEnv(t, llmtest.New(), Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	b.post(id, "saveas", "text")
	s.waitRegion(t, "modal", "Save As")
	b.post(id, "modal/close", "text")
	s.waitRegion(t, "modal", " hidden")
	ws := e.workspaces()[0]
	ws.with(func() {
		if ws.download != nil {
			t.Error("cancelling gives no file")
		}
	})
}

func TestLibraryRoundTrip(t *testing.T) {
	e := newEnv(t, llmtest.New(twoIssues()), Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	ws := e.workspaces()[0]

	// Check, then save to the library: the mistakes follow the text.
	b.post(id, "check", sampleText)
	s.waitRegion(t, "issues", "Issues (2)")
	eventually(t, "mistakes", func() bool { r, _ := e.stats.TypeStats(t.Context()); return len(r) == 2 })
	b.post(id, "save-library", sampleText)
	s.waitRegion(t, "modal", "Save to library", `value="He go home. She have a cat."`)
	b.postForm(id, "modal/submit", url.Values{"value": {"My essay"}})
	s.waitSignal(t, "_title", "My essay — English Tutor")
	docs, err := e.st.ListDocuments(t.Context())
	if err != nil || len(docs) != 1 || docs[0].Title != "My essay" || docs[0].Content != sampleText {
		t.Fatalf("documents = %+v, %v", docs, err)
	}
	ws.with(func() {
		if ws.sourceKey != "doc:1" {
			t.Errorf("source = %q, want doc:1", ws.sourceKey)
		}
	})
	events, err := e.stats.RecentExamples(t.Context(), "verb_form", 5)
	if err != nil || len(events) != 1 {
		t.Errorf("the mistakes stay counted once: %v, %v", events, err)
	}

	// The next Save to library updates the same document.
	b.post(id, "text", sampleText+" More.")
	b.post(id, "save-library", sampleText+" More.")
	eventually(t, "the update", func() bool {
		d, _ := e.st.GetDocument(t.Context(), 1)
		return d != nil && d.Content == sampleText+" More."
	})
	if docs, _ := e.st.ListDocuments(t.Context()); len(docs) != 1 {
		t.Errorf("there must still be one document, got %d", len(docs))
	}

	// A fresh text, then open the document from the library.
	b.post(id, "new", "")
	s.waitSignal(t, "text", "")
	b.post(id, "library/open", "")
	lib := s.waitRegion(t, "modal", "Library", "My essay")
	if !strings.Contains(lib, `data-id="1"`) {
		t.Errorf("the library lists the document: %q", lib)
	}
	b.post(id, "library/load?id=1", "")
	s.waitSignal(t, "text", sampleText+" More.")
	s.waitSignal(t, "_title", "My essay — English Tutor")
	s.waitRegion(t, "modal", " hidden")
	ws.with(func() {
		if ws.sourceKey != "doc:1" || ws.place.docID != 1 {
			t.Errorf("opened from the library: %q %+v", ws.sourceKey, ws.place)
		}
	})

	// Rename.
	cur := sampleText + " More."
	b.post(id, "library/open", cur)
	s.waitRegion(t, "modal", "Library")
	b.post(id, "library/rename?id=1", cur)
	s.waitRegion(t, "modal", "Rename document", `value="My essay"`)
	b.postForm(id, "modal/submit", url.Values{"value": {"Renamed"}})
	s.waitRegion(t, "modal", "Library", "Renamed")
	s.waitSignal(t, "_title", "Renamed — English Tutor")

	// Delete: the text stays, the next save creates a new document.
	b.post(id, "library/delete?id=1", cur)
	s.waitRegion(t, "modal", "The library is empty")
	ws.with(func() {
		if ws.place.docID != 0 || ws.text != sampleText+" More." {
			t.Errorf("after deleting the open document: %+v %q", ws.place, ws.text)
		}
	})
}

func TestLibraryErrors(t *testing.T) {
	e := newEnv(t, llmtest.New(), Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	b.post(id, "library/load?id=99", "")
	s.waitRegion(t, "banner", "no longer exists in the library")

	// Saving over a document that was deleted meanwhile reports it and detaches.
	doc := &store.Document{Title: "Gone", Content: "x"}
	if err := e.st.CreateDocument(t.Context(), doc); err != nil {
		t.Fatal(err)
	}
	b.post(id, "library/load?id=1", "")
	s.waitSignal(t, "text", "x")
	if err := e.st.DeleteDocument(t.Context(), doc.ID); err != nil {
		t.Fatal(err)
	}
	b.post(id, "save-library", "x")
	s.waitRegion(t, "banner", "no longer exists in the library")
	ws := e.workspaces()[0]
	ws.with(func() {
		if ws.place.docID != 0 {
			t.Error("the text is detached from the deleted document")
		}
	})
	b.post(id, "save-library", "x")
	s.waitRegion(t, "modal", "Save to library")
}

func TestUploadTooLargeIsRefused(t *testing.T) {
	e := newEnv(t, llmtest.New(), Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	if code := b.upload(id, "big.txt", bytes.Repeat([]byte("a"), maxUploadBytes+10)); code != 204 {
		t.Fatalf("open = %d", code)
	}
	s.waitRegion(t, "banner", "too large")
}

func TestOpenWithoutAFileIsReported(t *testing.T) {
	e := newEnv(t, llmtest.New(), Options{})
	b := e.browser()
	id, _ := b.open()
	s := b.stream(id)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("other", "x"); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	resp := b.do("POST", "/w/"+id+"/open", &buf, b.actionHeaders(mw.FormDataContentType()))
	_ = readBody(t, resp)
	if resp.StatusCode != 204 {
		t.Fatalf("open = %d", resp.StatusCode)
	}
	s.waitRegion(t, "banner", "The file could not be read.")
	// The failure is logged with its route, so that it can be replayed.
	if log := e.logs.String(); !strings.Contains(log, "FileReadFailed") || !strings.Contains(log, "opening a file") {
		t.Errorf("the log lacks the failure:\n%s", log)
	}
}
