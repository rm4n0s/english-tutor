# English Tutor

A desktop app for practising written English. You write a sentence or an essay in a text editor, an LLM finds the grammar, syntax and vocabulary errors, and the app underlines them. Select an error and the app explains why it is wrong and shows better ways to say it, in markdown, next to the editor. It also counts how often you make each kind of mistake, so you can see what to work on.

Written in Go with [miqt](https://github.com/mappu/miqt) (Qt 6 bindings) and [langchaingo](https://github.com/tmc/langchaingo). Works with Ollama, any OpenAI-compatible server, and Anthropic.

The full design is in [docs/SPEC.md](docs/SPEC.md).

## What it does

- **Underlines errors** by category, using colours and line styles that do not rely on colour alone: grammar is orange and wavy, vocabulary blue and dashed, syntax green and dotted. The legend above the editor hides or shows a category.
- **Issues list** next to the editor, with the same selection as the text: click a row or an underline and the other follows. `F7` and `Shift+F7` jump to the next and previous issue.
- **Explanations** streamed as markdown: what is wrong, why, better ways, and a tip. Cancel, Regenerate and Retry are one click away.
- **Statistics:** every mistake you make is counted by error type (repeats count every time), with 7 and 30 day views, examples of what you wrote, and a **Not an error** button for when the LLM was wrong. Token usage per LLM is shown in the status bar and in the Statistics dialog.
- **Check when you want:** the Check button always works; auto-check (after you pause typing) is optional and off by default.
- **Saving:** plain text and markdown files, and a library of documents in the app. Unsaved changes are asked about.

## Requirements

- Go 1.27 or newer (see `go.mod`).
- A C++ toolchain and the **Qt 6 development packages**, because miqt is a cgo binding and the build is not pure Go. `pkg-config` must find `Qt6Widgets`.
- A running LLM: for example [Ollama](https://ollama.com) with a model such as `llama3.1`, an OpenAI-compatible server, or an Anthropic API key.

Installing Qt 6:

| System | Command |
|---|---|
| Fedora | `sudo dnf install qt6-qtbase-devel gcc-c++ pkgconf-pkg-config` |
| Ubuntu / Debian | `sudo apt install qt6-base-dev g++ pkg-config` |
| macOS | `brew install qt@6 pkg-config` |
| Windows | See the [miqt README](https://github.com/mappu/miqt) (MSYS2 or Qt installer) |

Check with `pkg-config --modversion Qt6Widgets`.

## Build and run

```
go build -o english-tutor ./cmd/english-tutor
./english-tutor
```

The first build compiles the Qt bindings and takes several minutes; later builds are fast because Go caches the result.

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

## Your data and privacy

Everything is kept in `~/.english-tutor/` (mode `0700`):

- `english-tutor.db`, an SQLite database (mode `0600`) with your LLM list, library documents, mistake statistics, token usage and settings.
- `logs/english-tutor.log`, rotated at 5 MB with three old files kept. It records timings and token counts. It never contains your text, the LLM's answers or API keys.

**API keys are stored in plain text** in the database file. The file permissions are the only protection.

**The text you write is sent to the LLM you selected** when you check it or ask for an explanation. With a local Ollama server it stays on your machine; the Preferences dialog warns when a configuration points to a server on the internet.

Explanations come from an LLM and are not trusted: the app never loads images or other remote content from them, and a link in an explanation is opened only after you confirm its full address.

## Development

```
go build ./...
go vet ./...
golangci-lint run
go test -race ./...
gofmt -l .
```

All four checks must pass before work is merged. There is no CI: they are run locally. The Qt tests need no display, because they run with `QT_QPA_PLATFORM=offscreen` through `internal/uitest`; if Qt's offscreen plugin is missing they are skipped with a message.

Layout:

```
cmd/english-tutor/   main.go: starts the app
internal/config      ~/.english-tutor paths
internal/logging     slog with a rotated log file
internal/store       gorm models, numbered migrations, statistics, usage
internal/llm         langchaingo factory, token usage, fake model for tests
internal/analysis    prompts, parsing, locating, chunking, analyzer, explainer
internal/issues      issue state, selection, edit rebasing (no Qt)
internal/ui          Qt windows and dialogs
internal/app         wiring: checks, saving, preferences
internal/uitest      Qt test harness
internal/specs       keeps the spec's error table and the tests in step
docs/                SPEC.md, IMPLEMENTATION.md (plan), IMPLEMENTED.md (log)
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

## Known limits

- API keys are stored in plain text (an OS keyring is a possible later change).
- The explanation language is English only.
- Re-checking the same text counts each mistake once, but opening the same text from another place (a file after saving it to the library) counts it again, and inserting an identical snippet earlier in a text shifts the counting of later ones (see section 5.7 of the spec).
- The prompts have been tested with fake models and wire-level tests only, so small local models may need prompt tuning.
