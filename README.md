# English Tutor

A desktop app for practising written English. You write a sentence or an essay in a text editor, an LLM finds the grammar, syntax and vocabulary errors, and the app underlines them. Select an error and the app explains why it is wrong and shows better ways to say it, in markdown, next to the editor. It also counts how often you make each kind of mistake, so you can see what to work on.

Written in Go with [glutter](https://github.com/rm4n0s/glutter) (a Flutter-style toolkit for Linux, no Qt) and [langchaingo](https://github.com/tmc/langchaingo). Works with Ollama, any OpenAI-compatible server, and Anthropic.

**New here?** The [tutorial](docs/TUTORIAL.md) walks through the app with screenshots. The full design is in [docs/SPEC.md](docs/SPEC.md).

## What it does

- **Underlines errors** by category, using colours and line styles that do not rely on colour alone: grammar is orange and wavy, vocabulary blue and dashed, syntax green and dotted. The legend above the editor hides or shows a category.
- **Issues list** next to the editor, with the same selection as the text: click a row or an underline and the other follows. `F7` and `Shift+F7` jump to the next and previous issue.
- **Explanations** streamed as markdown: what is wrong, why, better ways, and a tip. Cancel, Regenerate and Retry are one click away.
- **Alternatives:** select a word, a sentence or a paragraph, right-click and choose **Alternative** to see other ways to write it in a list (double-click one to replace the selected text with it, or press Copy to put it on the clipboard). Not available in the browser version.
- **Statistics:** every mistake you make is counted by error type (repeats count every time), with 7 and 30 day views, examples of what you wrote, and a **Not an error** button for when the LLM was wrong. Token usage per LLM is shown in the status bar and in the Statistics dialog.
- **Check a selection:** select a sentence, right-click and choose **Check** (under Alternative) to check only that text; the issues in it are replaced and the rest stay. While a check runs a loading screen covers the editor.
- **Check when you want:** the Check button always works; auto-check (after you pause typing) is optional and off by default.
- **Saving:** plain text and markdown files, and a library of documents in the app. Unsaved changes are asked about.

## Requirements

- Go 1.27 or newer (see `go.mod`).
- Linux with a Wayland compositor or an X server, and an EGL driver with OpenGL ES 3.0 (Mesa 20 or newer, NVIDIA through glvnd, or llvmpipe). glutter calls the system libraries (Wayland, xkbcommon, libdecor) at run time, so no toolkit packages are needed to build. `GLUTTER_BACKEND=x11|wayland` chooses the backend.
- A C compiler (`gcc`), because the SQLite driver of gorm is a cgo binding.
- A running LLM: for example [Ollama](https://ollama.com) with a model such as `llama3.1`, an OpenAI-compatible server, or an Anthropic API key.

Installing the C compiler:

| System | Command |
|---|---|
| Fedora | `sudo dnf install gcc` |
| Ubuntu / Debian | `sudo apt install gcc` |

The application runs on Linux only.

## Build and run

```
go build -o english-tutor ./cmd/english-tutor
./english-tutor
```

The first build compiles SQLite and takes a minute or so; later builds are fast because Go caches the result.

### Install with `go install`

Without cloning the repository (the C compiler above is needed all the same):

```
go install github.com/rm4n0s/english-tutor/cmd/english-tutor@latest
english-tutor
```

The program is put in `$(go env GOBIN)`, or in `$(go env GOPATH)/bin` (usually `~/go/bin`) when `GOBIN` is not set; add that folder to your `PATH` to start it by name. There are no tagged versions yet, so `@latest` is the newest commit of `main`; use `@main` to say so, or a commit hash to pin one. From a clone, `go install ./cmd/english-tutor` does the same. The first build is slow, like `go build`.

### Add it to the application menu

To find English Tutor in your desktop's application menu (the "start menu"), run the installed program once with:

```
english-tutor -setup-desktop
```

It writes a desktop entry to `~/.local/share/applications/dev.rm4n0s.english-tutor.desktop` and an icon to `~/.local/share/icons/hicolor/64x64/apps/`, using `$XDG_DATA_HOME` instead of `~/.local/share` when that is set, and then exits. The entry points at the program you ran, so run it from where the program will stay; if you move it later, run the flag again. Delete the two files to remove the entry. Some desktops need a moment, or a new login, before the menu shows it.

## First steps

1. Click **Preferences** and add an LLM on the LLMs tab:
   - **Ollama:** provider `ollama`, base URL `http://localhost:11434`, model for example `llama3.1`. Run `ollama pull llama3.1` first.
   - **OpenAI-compatible:** the base URL including `/v1`, a model name, and a key if the server needs one.
   - **Anthropic:** an API key and a model name; the base URL can stay empty.

   Use **Test connection** before saving.
2. Write or paste some English text and press **Check** (`Ctrl+Return`).
3. Click an underline or a row in the issues list to read the explanation.

## Keyboard shortcuts

| Keys | Action |
|---|---|
| `Ctrl+Return` | Check, or cancel the running check |
| `F7` / `Shift+F7` | Next / previous issue |
| `Ctrl+N`, `Ctrl+O` | New, Open |
| `Ctrl+S`, `Ctrl+Shift+S` | Save, Save As |

## System tray

English Tutor stays in the system tray. Closing the window with the X button only hides it; your text and any running check are kept. Use the tray icon to come back: click it or choose **Open**. Choose **Close** in the tray menu to quit; if the text has unsaved changes you are asked to save, discard or cancel first.

Only one copy runs at a time. Starting English Tutor again, from the menu or a terminal, shows the window of the copy that is already running instead of opening a second tray icon.

If your desktop has no system tray (some GNOME setups need an AppIndicator extension), the X button quits the application as usual.

## Web server

The same window can be used in a web browser. Start the server with the **Start web server** button of the window, or the same item of the tray menu (**Open web page** then opens it); the same controls stop it. It is off until you start it, and quitting the application stops it.

- **Port:** 3333 by default. Change it on the **Web server** tab of Preferences.
- **Who may connect:** this computer, at `http://localhost:3333/`, needs nothing. Other computers are refused unless you set a password on the same tab; then they get a sign-in page. Without a password the server only listens on the loopback addresses. The page is plain HTTP, so use a password only on a network you trust, and do not put a reverse proxy on this computer in front of a server without a password (every request would look local).
- **Differences:** Open is the browser's file chooser and Save / Save As download the text; unsaved changes are asked about with the browser's own question; API keys are never sent to the browser; the port and the password can only be changed in the window.
- **Several tabs** each have their own text and issues. The LLM list, the library and the statistics are shared with the window. A tab keeps its state when you reload it, for 12 hours without a browser, and until the server stops.
- **Log:** every request is logged in `logs/english-tutor.log` (method, route, address, status, time, allowed or denied). Never the text, the query or the address of a page.

The pages use [Datastar](https://data-star.dev) (v1.0.4, MIT licence), vendored in `internal/web/static/`, so they work without internet access.

## Your data and privacy

Everything is kept in `~/.english-tutor/` (mode `0700`):

- `english-tutor.db`, an SQLite database (mode `0600`) with your LLM list, library documents, mistake statistics, token usage and settings.
- `logs/english-tutor.log`, rotated at 5 MB with three old files kept. It records timings and token counts. It never contains your text, the LLM's answers or API keys.

**API keys are stored in plain text** in the database file. The file permissions are the only protection.

**The text you write is sent to the LLM you selected** when you check it, ask for an explanation or ask for alternatives. With a local Ollama server it stays on your machine; the Preferences dialog warns when a configuration points to a server on the internet.

Explanations come from an LLM and are not trusted: the app never loads images or other remote content from them, and a link in an explanation is opened only after you confirm its full address.

## Development

```
go build ./...
go vet ./...
golangci-lint run
go test -race ./...
gofmt -l .
```

All four checks must pass before work is merged. There is no CI: they are run locally. The window tests need no display: they run on glutter's test host (`gluttertest`), with a fake clock.

Layout:

```
cmd/english-tutor/   main.go: starts the app
internal/config      ~/.english-tutor paths
internal/logging     slog with a rotated log file
internal/store       gorm models, numbered migrations, statistics, usage
internal/llm         langchaingo factory, token usage, fake model for tests
internal/analysis    prompts, parsing, locating, chunking, analyzer, explainer
internal/issues      issue state, selection, edit rebasing (no GUI)
internal/gui         glutter windows, dialogs, editor, tray
internal/web         the web server: access rules, per-tab state, Datastar pages (no GUI)
internal/app         wiring: checks, saving, preferences
internal/view        what the windows show, without a toolkit (colours, formats)
internal/specs       keeps the spec's error table and the tests in step
docs/                SPEC.md, TUTORIAL.md (with images/), IMPLEMENTATION.md (plan), IMPLEMENTED.md (log)
```

Conventions: errors use `github.com/rm4n0s/errors` with a tag and a route test per failure (a test checks that every tag in the spec's error table has one), tests are table-driven, there is no global mutable state, `context.Context` comes first in anything that does I/O, and all SQL goes through gorm with numbered migrations.

## Manual smoke check

The automated tests use fake LLMs and an offscreen display. Before a release, run through this on a real display with a real LLM:

1. Start the app with an empty `~/.english-tutor`: the explanation panel invites you to open Preferences and Check is disabled.
2. Add an LLM, press **Test connection**, save, and check that it is selected after a restart.
3. Write a text with a few errors and press **Check**: underlines appear in three styles, the list shows the same issues, the status bar shows counts and tokens.
4. Click an underline, then a list row, then press `F7`: the same issue is selected everywhere and its explanation streams in.
5. Hover an underline for its tooltip. Hide a category in the legend: its underlines and rows go.
6. Press **Check**, then **Cancel** while it runs: the old issues stay and nothing is reported as an error. Stop the LLM server and check again: a banner shows the failure.
7. Edit inside an underlined word: the underline disappears at once. Type before it: the other underlines move with the text.
8. **Not an error**, **Regenerate**, and a link in an explanation (a confirmation shows the full address).
9. Turn auto-check on, type, pause: a check starts after the delay and is not counted in the statistics. Turn it off again.
10. Open **Statistics** after two manual checks of the same text: each mistake is counted once; sorting, filters, examples and the Token usage tab work; Reset asks first.
11. Save, Save As, Open, Save to library, the Library dialog (open, rename, delete), and closing with unsaved changes.
12. Try a dark theme and a narrow window.
13. Press the X button: the window goes and a tray icon stays. **Open** (or a click on the icon) brings the same text back. With unsaved changes, tray **Close** asks first and quits; without a tray, X quits.
14. Press **Start web server** (or the tray item) and open the page in a browser: write, Check, click an underline and a row, press `F7`, open Preferences, Statistics and the Library, upload a file with Open and download with Save. Start a second tab: it has its own text. Press **Stop web server**: the pages stop answering.
15. Occupy port 3333 with another program and press **Start web server**: a banner names the port. Set a password on the Web server tab and open the page from another computer: it asks for the password, and five wrong ones lock that computer out for a while.
16. Select a single word, right-click and choose **Alternative**: the Alternatives panel opens at once with a busy indicator and then lists a few replacements. Do the same with a paragraph, and with only a space selected (no such entry). Double-click a row: the selection is replaced and the window closes; `Ctrl+Z` brings the old text back. Press Copy on another row and paste. Close the window while it is still loading: nothing is reported as an error.
17. Select the second sentence of a checked text, right-click and choose **Check**: a grey layer with a busy indicator covers the editor, typing does nothing, and the status bar says `Checking...`. When it ends the layer goes; underlines in the other sentences are unchanged and the ones in the selection are the new result. Do it again and press **Cancel** while it runs: the layer goes and nothing changes. With only a space selected, or no LLM configured, there is no usable Check entry.

## Known limits

- API keys are stored in plain text (an OS keyring is a possible later change).
- The explanation language is English only.
- Re-checking the same text counts each mistake once, but opening the same text from another place (a file after saving it to the library) counts it again, and inserting an identical snippet earlier in a text shifts the counting of later ones (see section 5.7 of the spec).
- The prompts have been tested with fake models and wire-level tests only, so small local models may need prompt tuning.
