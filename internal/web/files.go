package web

import (
	"bytes"
	"fmt"
	"mime"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/english-tutor/internal/issues"
	"github.com/rm4n0s/english-tutor/internal/store"
)

// maxUploadBytes is the largest text file that is opened, as on the desktop.
const maxUploadBytes = 10 << 20

const (
	lineEndingLF   = "\n"
	lineEndingCRLF = "\r\n"
)

// decodeUpload turns the bytes of an uploaded file into text. It returns the
// text with "\n" line endings, whatever the file used, and the line ending to
// write back. A leading byte order mark is dropped.
//
// Errors: FileReadFailed (too large), FileNotUTF8 (not valid UTF-8, or binary).
// Their messages are written for the user.
func decodeUpload(data []byte) (text, lineEnding string, err error) {
	if len(data) > maxUploadBytes {
		return "", "", errors.New("FileReadFailed",
			"The file is too large to open as text (the limit is 10 MB).", "size", len(data))
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(data) {
		return "", "", errors.New("FileNotUTF8", "This file is not valid UTF-8 text, so it was not opened.")
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return "", "", errors.New("FileNotUTF8", "This file does not look like a text file, so it was not opened.")
	}
	text = string(data)
	lineEnding = lineEndingLF
	if strings.Contains(text, "\r\n") {
		lineEnding = lineEndingCRLF
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return text, lineEnding, nil
}

// safeFileName makes a name the browser may save under: no folders, no
// characters a file system refuses, and an extension (.txt when there is none).
func safeFileName(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	name = path.Base(name)
	name = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r), strings.ContainsRune(`<>:"|?*/`, r):
			return -1
		}
		return r
	}, name)
	name = strings.Trim(name, " .")
	if name == "" || name == "." {
		name = untitled
	}
	if path.Ext(name) == "" {
		name += ".txt"
	}
	return name
}

// defaultDocumentTitle is the title proposed for a new library document: the
// first line of the text, shortened to 60 characters.
func defaultDocumentTitle(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			return issues.ShortenSnippet(line, titleRune)
		}
	}
	return untitled
}

// queueDownload hands the browser a file to save. The page fetches it once, from
// an address nobody else knows.
func (w *workspace) queueDownload(name string, content []byte) {
	w.download = &pendingDownload{
		token:   randomToken()[:22],
		name:    name,
		content: content,
		expires: w.srv.now().Add(time.Minute),
	}
	w.touch(rDownload)
}

// contentDisposition is the header that makes the browser save the file.
func contentDisposition(name string) string {
	return mime.FormatMediaType("attachment", map[string]string{"filename": name})
}

// takeDownload returns the file with this token and forgets it.
func (w *workspace) takeDownload(token string) (*pendingDownload, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	d := w.download
	if d == nil || d.token != token || w.srv.now().After(d.expires) {
		return nil, false
	}
	w.download = nil
	return d, true
}

// onNew starts an empty text.
func (w *workspace) onNew() {
	w.place = place{}
	w.lineEnding = lineEndingLF
	w.loadText("")
	w.sourceKey = newSessionKey()
}

// onOpen shows an uploaded file in the editor. The file is read before anything
// is replaced, so a file that cannot be opened leaves the current text alone.
func (w *workspace) onOpen(name string, data []byte) {
	text, ending, err := decodeUpload(data)
	if err != nil {
		w.showError("opening a file", err)
		return
	}
	w.place = place{name: path.Base(strings.ReplaceAll(name, `\`, "/"))}
	w.lineEnding = ending
	w.loadText(text)
	// A browser does not tell the server where a file lives, so an opened file
	// is a new source, like a new text (section 7).
	w.sourceKey = newSessionKey()
}

// save gives the browser the text as a file: under the name of the text, or
// after asking for one if it has none (section 7).
func (w *workspace) save() {
	if w.place.name == "" {
		w.saveAs()
		return
	}
	w.sendFile(safeFileName(w.place.name))
}

// saveAs asks for a file name. A text with none is proposed its first line.
func (w *workspace) saveAs() {
	name := w.place.name
	if name == "" {
		name = strings.TrimRight(defaultDocumentTitle(w.text), ". ") + ".txt"
	}
	w.openPrompt(&prompt{
		purpose: promptSaveAs, Title: "Save As", Label: "File name", Value: name, Button: "Download",
	})
}

// sendFile queues the text as a download and marks it saved.
func (w *workspace) sendFile(name string) {
	text := w.text
	if w.lineEnding == lineEndingCRLF {
		text = strings.ReplaceAll(text, "\n", lineEndingCRLF)
	}
	w.place.name = name
	w.savedText = w.text
	w.queueDownload(name, []byte(text))
	w.touch(rSignals)
}

// saveToLibrary stores the text in the library: a new document (asking for a
// title) or, if the text came from the library, an update of that document.
func (w *workspace) saveToLibrary() {
	if w.place.docID == 0 {
		w.openPrompt(&prompt{
			purpose: promptLibraryTitle, Title: "Save to library",
			Label: "Title", Value: defaultDocumentTitle(w.text), Button: "Save",
		})
		return
	}
	doc := &store.Document{ID: w.place.docID, Title: w.place.docTitle, Content: w.text}
	if err := w.srv.deps.Store.UpdateDocument(w.ctx(), doc); err != nil {
		if appErr, ok := errors.FromError(err); ok && appErr.HasRoute("DocumentNotFound") {
			// Deleted from the library in the meantime: the next save creates
			// a new document.
			w.place.docID, w.place.docTitle = 0, ""
		}
		w.showError("saving to the library", err)
		return
	}
	w.savedToLibrary(w.place.docID, w.place.docTitle)
}

// createDocument is the title prompt of Save to library.
func (w *workspace) createDocument(title string) error {
	doc := &store.Document{Title: title, Content: w.text}
	if err := w.srv.deps.Store.CreateDocument(w.ctx(), doc); err != nil {
		return err
	}
	w.savedToLibrary(doc.ID, doc.Title)
	return nil
}

// savedToLibrary ties the text to the document it was saved in. The mistakes
// recorded so far follow the text to its new source (section 5.7).
func (w *workspace) savedToLibrary(id uint, title string) {
	w.place.docID, w.place.docTitle, w.place.name = id, title, title
	w.setSource(fmt.Sprintf("doc:%d", id), true)
	w.savedText = w.text
	w.touch(rSignals)
}

// setSource switches the statistics source of the text to key. With move set,
// the mistakes recorded under the old source follow the text; without it the
// text simply starts a new source.
func (w *workspace) setSource(key string, move bool) {
	old := w.sourceKey
	w.sourceKey = key
	if move && old != key {
		if err := w.srv.deps.Stats.MoveSource(w.ctx(), old, key); err != nil {
			w.showError("moving statistics", err)
		}
	}
}

// libraryOpen loads a library document into the editor.
func (w *workspace) libraryOpen(id uint) {
	doc, err := w.srv.deps.Store.GetDocument(w.ctx(), id)
	if err != nil {
		w.showError("opening a library document", err)
		w.refreshLibrary()
		return
	}
	w.place = place{docID: doc.ID, docTitle: doc.Title, name: doc.Title}
	w.lineEnding = lineEndingLF
	w.loadText(doc.Content)
	w.setSource(fmt.Sprintf("doc:%d", doc.ID), false)
	w.closeModal()
}

// libraryRename gives a document a new title.
func (w *workspace) libraryRename(id uint, title string) error {
	doc, err := w.srv.deps.Store.GetDocument(w.ctx(), id)
	if err != nil {
		return err
	}
	doc.Title = title
	if err := w.srv.deps.Store.UpdateDocument(w.ctx(), doc); err != nil {
		return err
	}
	if w.place.docID == id {
		w.place.docTitle = title
		if strings.HasPrefix(w.sourceKey, "doc:") {
			w.place.name = title
		}
		w.touch(rSignals)
	}
	return nil
}

// libraryDelete removes a document. The text that was tied to it stays in the
// editor, and the next Save to library creates a new document.
func (w *workspace) libraryDelete(id uint) {
	if err := w.srv.deps.Store.DeleteDocument(w.ctx(), id); err != nil {
		w.showError("deleting a library document", err)
		w.refreshLibrary()
		return
	}
	if w.place.docID == id {
		w.place.docID, w.place.docTitle = 0, ""
	}
	w.refreshLibrary()
}
