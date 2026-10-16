package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/english-tutor/internal/analysis"
)

// maxBody bounds a request: a text of the largest size with JSON escaping, or an
// uploaded file of the largest size, with room for the rest.
const maxBody = 2*maxTextBytes + 1<<20

// actionCtx is one request that does something to a workspace.
type actionCtx struct {
	w    *workspace
	req  *http.Request
	form url.Values
}

// arg returns a value from the address, or else from the form.
func (c *actionCtx) arg(name string) string {
	if v := c.req.URL.Query().Get(name); v != "" {
		return v
	}
	return c.form.Get(name)
}

func (c *actionCtx) intArg(name string) (int, bool) {
	n, err := strconv.Atoi(c.arg(name))
	return n, err == nil
}

// uintArg is a record id.
func (c *actionCtx) uintArg(name string) (uint, bool) {
	n, err := strconv.ParseUint(c.arg(name), 10, 32)
	return uint(n), err == nil
}

// upload returns the file sent with the request.
func (c *actionCtx) upload() (name string, data []byte, err error) {
	f, hdr, err := c.req.FormFile("file")
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = f.Close() }()
	data, err = io.ReadAll(io.LimitReader(f, maxUploadBytes+1))
	return hdr.Filename, data, err
}

// action wraps a handler that changes a workspace. It finds the workspace, reads
// the signals the page sent (the text of the editor among them), brings the
// workspace up to date with that text, runs fn, and answers 204: what changed
// reaches the page through its event stream.
func (s *Server) action(r *run, fn func(c *actionCtx)) http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Datastar-Request") != "true" {
			http.Error(rw, "Bad request.", http.StatusBadRequest)
			return
		}
		ws, ok := r.workspaceFor(req.PathValue("id"), cookieValue(req))
		if !ok {
			http.Error(rw, "This page is no longer available.", http.StatusNotFound)
			return
		}
		req.Body = http.MaxBytesReader(rw, req.Body, maxBody)
		c := &actionCtx{w: ws, req: req, form: url.Values{}}
		var text *string
		ct := req.Header.Get("Content-Type")
		var err error
		switch {
		case strings.HasPrefix(ct, "application/json"):
			var sig struct {
				Text *string `json:"text"`
			}
			if err = json.NewDecoder(req.Body).Decode(&sig); err == nil {
				text = sig.Text
			} else if errors.Is(err, io.EOF) {
				err = nil
			}
		case strings.HasPrefix(ct, "multipart/form-data"):
			if err = req.ParseMultipartForm(maxUploadBytes); err == nil {
				c.form = req.MultipartForm.Value
			}
		case strings.HasPrefix(ct, "application/x-www-form-urlencoded"):
			if err = req.ParseForm(); err == nil {
				c.form = req.PostForm
			}
		}
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				http.Error(rw, "The request is too large.", http.StatusRequestEntityTooLarge)
			} else {
				http.Error(rw, "Bad request.", http.StatusBadRequest)
			}
			return
		}

		ws.mu.Lock()
		if ws.closed {
			ws.mu.Unlock()
			http.Error(rw, "This page is no longer available.", http.StatusNotFound)
			return
		}
		ws.lastSeen = s.now()
		if text != nil {
			ws.syncText(*text)
		}
		fn(c)
		ws.mu.Unlock()
		rw.WriteHeader(http.StatusNoContent)
	}
}

// actions registers everything the page can ask for, one route each.
func (s *Server) actions(r *run, mux *http.ServeMux) {
	act := func(path string, fn func(c *actionCtx)) {
		mux.HandleFunc("POST /w/{id}/"+path, s.action(r, fn))
	}

	// The editor.
	act("text", func(*actionCtx) {})
	act("cursor", func(c *actionCtx) {
		if pos, ok := c.intArg("pos"); ok {
			c.w.cursorAt(pos)
		}
	})
	act("check", func(c *actionCtx) {
		if !c.w.checking {
			c.w.startCheck(true)
		}
	})
	act("cancel", func(c *actionCtx) { c.w.cancelCheck() })
	act("llm", func(c *actionCtx) {
		if id, ok := c.uintArg("id"); ok {
			c.w.selectLLM(id)
			c.w.rememberLLM(id)
		}
	})
	act("auto", func(c *actionCtx) { c.w.setAutoCheckSetting(c.arg("on") == "true") })
	act("legend", func(c *actionCtx) {
		for _, cat := range analysis.Categories() {
			if string(cat) == c.arg("c") {
				c.w.toggleCategory(cat)
			}
		}
	})
	act("banner/dismiss", func(c *actionCtx) { c.w.setBanner("") })

	// The issues and the explanation.
	act("issue/select", func(c *actionCtx) {
		if id, ok := c.intArg("id"); ok {
			c.w.selectIssue(id)
		}
	})
	act("issue/step", func(c *actionCtx) {
		pos, _ := c.intArg("pos")
		c.w.stepIssue(c.arg("dir") != "prev", pos)
	})
	act("issue/not-an-error", func(c *actionCtx) { c.w.notAnError() })
	act("explain/regenerate", func(c *actionCtx) { c.w.regenerate() })
	act("explain/cancel", func(c *actionCtx) { c.w.cancelExplain() })

	// Files and the library.
	act("new", func(c *actionCtx) { c.w.onNew() })
	act("open", func(c *actionCtx) {
		name, data, err := c.upload()
		if err != nil {
			c.w.showError("opening a file", errors.NewErr("FileReadFailed", err).
				SetMessage("The file could not be read."))
			return
		}
		c.w.onOpen(name, data)
	})
	act("save", func(c *actionCtx) { c.w.save() })
	act("saveas", func(c *actionCtx) { c.w.saveAs() })
	act("save-library", func(c *actionCtx) { c.w.saveToLibrary() })
	act("library/open", func(c *actionCtx) { c.w.openLibrary() })
	act("library/load", func(c *actionCtx) {
		if id, ok := c.uintArg("id"); ok {
			c.w.libraryOpen(id)
		}
	})
	act("library/rename", func(c *actionCtx) {
		if id, ok := c.uintArg("id"); ok {
			c.w.startRename(id)
		}
	})
	act("library/delete", func(c *actionCtx) {
		if id, ok := c.uintArg("id"); ok {
			c.w.libraryDelete(id)
		}
	})
	act("modal/close", func(c *actionCtx) { c.w.closeModal() })
	act("modal/submit", func(c *actionCtx) { c.w.submitPrompt(c.form.Get("value")) })

	// Preferences.
	act("prefs/open", func(c *actionCtx) { c.w.openPrefs("llms") })
	act("prefs/tab", func(c *actionCtx) { c.w.prefsTab(c.arg("tab")) })
	act("prefs/llm/edit", func(c *actionCtx) {
		if id, ok := c.uintArg("id"); ok {
			c.w.editLLM(id)
		}
	})
	act("prefs/llm/form", func(c *actionCtx) { c.w.llmFormChanged(c.form.Get) })
	act("prefs/llm/save", func(c *actionCtx) { c.w.saveLLM(c.form.Get) })
	act("prefs/llm/test", func(c *actionCtx) { c.w.testLLM(c.form.Get) })
	act("prefs/llm/delete", func(c *actionCtx) {
		if id, ok := c.uintArg("id"); ok {
			c.w.deleteLLM(id)
		}
	})
	act("prefs/general", func(c *actionCtx) { c.w.saveGeneral(c.form.Get) })

	// Statistics.
	act("stats/open", func(c *actionCtx) { c.w.openStats() })
	act("stats/tab", func(c *actionCtx) { c.w.statsTab(c.arg("tab")) })
	act("stats/filter", func(c *actionCtx) { c.w.statsFilter(c.form.Get("category"), c.form.Get("period")) })
	act("stats/sort", func(c *actionCtx) {
		if col, ok := c.intArg("col"); ok {
			c.w.statsSort(col)
		}
	})
	act("stats/select", func(c *actionCtx) { c.w.statsSelect(c.arg("type")) })
	act("stats/reset", func(c *actionCtx) { c.w.statsReset(c.arg("which")) })
}

// rememberLLM stores the choice of the selector like the desktop window does.
func (w *workspace) rememberLLM(id uint) {
	if w.llmID != id {
		return
	}
	if err := w.srv.deps.Store.SetSelectedLLMID(w.ctx(), id); err != nil {
		w.logError("saving selected LLM", err)
		return
	}
	w.notifyDesktop()
}
