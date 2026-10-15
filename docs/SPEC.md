# English Tutor: Specification

Status: draft v0.1

## 1. Purpose

A desktop application for practising written English. The user writes a sentence or an essay in a text editor. An LLM finds grammar, vocabulary and syntax errors, and the editor underlines them, with a different colour and line style for grammar, vocabulary and syntax errors. When the user selects an error, a side panel explains why it is wrong and shows better alternatives, rendered from the LLM's markdown.

## 2. Goals and non-goals

**Goals**
- Write English text in a plain editor with LLM-detected errors underlined, coloured by category.
- Learn from each error: a markdown explanation and better alternatives, next to the editor.
- Choose which LLM is used from a selector above the editor.
- Manage the LLM list in a preferences dialog, stored in SQLite.
- Save the text to a file and to an in-app document list.
- Score the user by error type: count how many times each kind of mistake was made, so the user sees which mistakes they repeat.

**Non-goals (v1)**
- No rich text formatting; the editor is plain text.
- No offline/rule-based checker (LanguageTool etc.); every error comes from the LLM.
- No languages other than English as the text being checked.
- No accounts, sync or cloud storage.
- No mobile or web version.
- No lessons, exercises or grades. Progress tracking is limited to the per-error-type counts in section 5.7.

## 3. Technology

| Concern | Choice |
|---|---|
| Language | Go |
| GUI | `github.com/mappu/miqt` (Qt bindings), Qt 6 |
| LLM access | `github.com/tmc/langchaingo` (`llms/ollama`, `llms/openai`, `llms/anthropic`) |
| ORM | `gorm.io/gorm` (all queries through its builder) |
| Database | SQLite, through a gorm SQLite driver (see below) |
| Errors | `github.com/rm4n0s/errors` |

Dependencies and constraints:
- miqt needs cgo and a C++ toolchain with the Qt 6 development packages installed, so the build is not pure Go. Build instructions go in the README.
- The gorm SQLite driver is chosen at implementation time. `gorm.io/driver/sqlite` (cgo) is the default because cgo is already required. A pure-Go driver is an alternative.
- CLAUDE.md requires approval before adding dependencies beyond those named by the user. Each of the gorm driver, any markdown helper and any test helper needs approval when it is first added.
- All database access goes through gorm's builder. The schema is created by numbered Go migrations in `internal/store`. No hand-written SQL.

## 4. User interface

### 4.1 Main window

```
+----------------------------------------------------------------------+
| LLM: [ Claude Sonnet v ]  [Check]  [ ] Auto-check  [Stats] [Prefs]   |
| [New] [Open] [Save] [Save As] [Save to library]                      |
| Legend:  ~~ grammar   -- vocabulary   .. syntax                      |
+----------------------------------------+-----------------------------+
|                                        | Issues (3)                  |
|  Text editor                           | [G] He go to school         |
|  (errors underlined by category)       | [V] make a photo            |
|                                        | [S] I very like it          |
|                                        |-----------------------------|
|                                        | Subject-verb agreement      |
|                                        | Markdown theory and         |
|                                        | solutions of the issue      |
|                                        | selected in the list        |
+----------------------------------------------------------------------+
| 3 issues | 142 words | tokens in 812 / out 164                       |
+----------------------------------------------------------------------+
```

- The window is a `QMainWindow`. The editor and the right column sit in a horizontal `QSplitter`, with the editor on the left. The right column is a vertical `QSplitter` with the issues list (section 4.3) on top and the explanation panel (section 4.4) below. Both splitter positions are remembered.
- **Toolbar, row 1:** LLM selector (`QComboBox`), Check button, Auto-check checkbox (optional, **off by default**; the Check button always works), Statistics button (section 4.6), Preferences button. While a check is running, the Check button turns into a **Cancel** button (section 5.1).
- **Toolbar, row 2:** file and library buttons (section 7).
- The LLM selector lists the configured LLMs by name. The last selection is remembered. If the list is empty, the selector is disabled, Check is disabled, and the explanation panel shows a markdown message that links to Preferences.
- The status bar shows the issue count, the check state (idle, checking, cancelled, failed), the word count, and the input / output tokens of the last request and of the session (section 5.8).

### 4.2 Text editor

- A `QPlainTextEdit` with a monospace-free, comfortable proportional font and word wrap.
- Detected errors are underlined using `ExtraSelections` with `QTextCharFormat`. The colour and the line style depend on the error's category, so errors can be told apart by more than colour (colour-blind users, and a single red for everything is confusing):

  | Category | Colour (light theme / dark theme) | Underline style |
  |---|---|---|
  | grammar | vermilion `#D55E00` / `#FF8A4C` | wavy (`WaveUnderline`) |
  | vocabulary | blue `#0072B2` / `#56B4E9` | dashed (`DashUnderline`) |
  | syntax | bluish green `#009E73` / `#2FD3A2` | dotted (`DotLine`) |

  The colours come from the Okabe-Ito colour-blind-safe palette. The light or dark variant is chosen from the application palette. Colours are fixed in v1 and are not configurable. There are three colours rather than one per error type because about twenty colours cannot be told apart; the specific error type is given by the tooltip and the explanation header. The underlines never change the document text or undo stack.
- The cursor moving into an underlined range, or a click on it, selects that issue, which is then also selected in the issues list (section 4.3). Clicking elsewhere clears the selection and returns the explanation panel to its idle state.
- The selected issue is drawn with a stronger highlight (a background of the category colour at about 20% opacity) in addition to the underline.
- Hovering an underline shows a tooltip with the error type label, the issue's short message and the category.
- A small legend (three coloured samples with their category names) sits above the editor, next to the toolbar, so the colours are explained on screen. Clicking a legend entry hides or shows the underlines of that category.
- Keyboard: `F7` / `Shift+F7` jump to the next / previous issue.
- Editing invalidates issues (section 5.5).

### 4.3 Issues list

A list of all the errors found by the last check sits at the top of the right column, above the explanation panel. The user can work through the errors from the list, without having to find each underline in the text.

- A `QListWidget` with one row per placed issue, in the order they appear in the text.
- Each row shows:
  - a category marker in the category colour with a letter (**G**rammar, **V**ocabulary, **S**yntax), so the category is not shown by colour alone;
  - the wrong snippet, shortened to about 40 characters;
  - on a second line, the error type label and a count badge with how often the user has made that error type (`x7`, from the statistics in section 5.7; hidden when the issue is not recorded).
- The header reads "Issues (N)". When categories are hidden through the legend (section 4.2), it reads "Issues (3 of 5 shown)" and the hidden categories are removed from the list too. The legend toggles are the only filter, so there is one place to set it.
- **One shared selection.** The editor and the list select the same issue:
  - Clicking a row, or moving through the list with the Up / Down keys, selects the issue. The editor scrolls to it, highlights it, and puts the text cursor at the start of the range, without taking keyboard focus from the list.
  - Selecting an underline in the editor, or using `F7` / `Shift+F7`, selects the matching row and scrolls it into view.
  - Selecting an issue, from either place, starts its explanation (section 5.4) and shows it in the explanation panel (section 4.4).
- Double-clicking a row moves keyboard focus to the editor, with the cursor at the start of the issue.
- The right-click menu on a row has **Not an error** (section 5.7) and **Regenerate explanation**.
- Empty states are shown as a single grey line in the list: before the first check "Press Check to find errors", while checking "Checking...", and after a check without issues "No errors found".
- Rows disappear immediately when an edit removes their issue (section 5.5). Issues the locator could not place (section 5.3) are not listed; the status bar counts them.
- The issue state (the list of placed issues and the selected one) is a plain Go type that the editor and the list both observe. The list and the editor never talk to each other directly.

### 4.4 Explanation panel

- A `QTextBrowser` showing the markdown via `SetMarkdown`. Remote images and other remote content are never fetched. Clicking a link (http/https only) first shows a confirmation dialog with the full URL, because the text comes from an LLM (section 5.9).
- States:
  - **Idle:** short hint ("Select an error in the list or an underlined error in the text to learn more").
  - **Loading:** shows the selected snippet and category, plus a progress indicator and a **Cancel** button while the explanation streams in. Cancel returns the panel to the idle state with a Regenerate button.
  - **Ready:** the markdown explanation. Streamed tokens re-render the markdown as they arrive, throttled to about 10 renders per second.
  - **Failed:** an error message and a Retry button.
- Above the explanation, a header line shows the error type's label and how often the user has made it: "Subject-verb agreement: you have made this mistake 7 times (2 in the last 7 days)". The numbers come from the statistics (section 5.7).
- A **Not an error** button removes the issue's underline and its recorded count, for when the LLM was wrong (section 5.7).
- The explanation is requested only when an issue is selected, from the list or from the text (section 5.4), not for every issue during a check.
- Explanations are cached in memory per issue for the lifetime of that check result. Re-selecting an issue shows the cached text. A "Regenerate" button asks the LLM again.

### 4.5 Preferences dialog

- A modal `QDialog` opened from the Preferences button.
- **LLMs tab:** a list of configured LLMs with Add, Edit, Delete, and Test connection. The edit form fields:

  | Field | Notes |
  |---|---|
  | Name | Unique, shown in the selector |
  | Provider | `ollama`, `openai-compatible`, `anthropic` |
  | Model | Free text, for example `llama3.1`, `gpt-4o`, `claude-sonnet-5-5` |
  | Base URL | Required for `ollama` (default `http://localhost:11434`) and `openai-compatible`; optional for `anthropic` |
  | API key | Required for `anthropic` and usually for `openai-compatible`; not used by `ollama`. Entered in a password field |
  | Temperature | Optional, default 0.2 |

- **Test connection** sends a tiny prompt and reports success or the failure reason. It never blocks the UI thread.
- **General tab:** auto-check on/off (default off), auto-check delay in seconds (default 3, used only when auto-check is on), editor font size.
- Deleting the currently selected LLM selects another one, or leaves the selector empty if none remain.

### 4.6 Statistics dialog

- Opened from the Statistics button.
- A table with one row per error type that has at least one recorded mistake:

  | Column | Meaning |
  |---|---|
  | Error type | Label from the taxonomy (section 5.6), for example "Missing article" |
  | Category | grammar, vocabulary or syntax |
  | Total | all-time count |
  | Last 7 days / Last 30 days | counts in those windows |
  | Last seen | date of the most recent mistake |

- Sorted by Total, highest first. Clicking a column header sorts by it. A category filter and a period selector (all time, 30 days, 7 days) apply to the table.
- A summary line above the table shows the total mistakes in the selected period and the most frequent error type.
- Selecting a row shows the three most recent example snippets for that type (the user's own wrong text), so the user can see what they actually wrote.
- A **Reset statistics** button deletes all recorded mistakes after a confirmation.
- A second tab, **Token usage**, shows input and output tokens per LLM for all time, the last 30 days and the last 7 days (section 5.8), with its own Reset button.

## 5. Error analysis

### 5.1 Pipeline

1. The user presses Check (or auto-check fires).
2. The app snapshots the editor text and a revision counter, and sends the check prompt to the selected LLM.
3. The LLM replies with a JSON list of issues.
4. The app validates the JSON, finds each issue's position in the snapshot text (section 5.3), and drops issues it cannot place.
5. If the document is still at the same revision, the UI replaces the underlines. If the user kept typing, the result is discarded (or rebased, see section 5.5).

Only one check runs at a time. Starting a new check cancels the previous one through its `context.Context`.

**Cancel:** while a check runs, the Check button becomes a Cancel button. Pressing it cancels the request's context. A cancelled check:
- leaves the existing underlines and the recorded mistakes exactly as they were, and discards results from chunks that already finished;
- shows "Check cancelled" in the status bar, with no error banner;
- is detected by checking `ctx.Err()` for `context.Canceled` before a failure is turned into an error, so cancelling never produces an error tag. A timeout is different and still produces `LLMCallTimeout`.

The explanation panel has its own Cancel button while an explanation streams (section 4.4).

### 5.2 Check prompt and response contract

The system prompt tells the model to act as an English teacher, to find only real grammar, vocabulary and syntax errors, to ignore style preferences, and to answer with JSON only. The response contract:

```json
{
  "issues": [
    {
      "text": "He go to school",
      "error_type": "subject_verb_agreement",
      "message": "Subject-verb agreement: 'go' should be 'goes'.",
      "context_before": "Yesterday I saw that ",
      "context_after": " every day."
    }
  ]
}
```

- `text` is the exact substring of the user's text that contains the error. It should be as short as possible while still being the minimal faulty span.
- `error_type` is one identifier from the fixed taxonomy in section 5.6, which the prompt lists. The category (`grammar`, `vocabulary`, `syntax`) is not sent by the model; the app derives it from the taxonomy.
- `message` is one short sentence, shown as the tooltip. The full explanation comes later.
- `context_before` / `context_after` are up to about 30 characters of surrounding text, used only to pick the right occurrence when `text` appears more than once.

LLMs are unreliable at counting character offsets, so the model is never asked for offsets. The app computes them.

The prompt and the response structs live in `internal/analysis`. For long texts the app splits the input into chunks at paragraph boundaries (target at most about 2000 words per request), runs the chunks sequentially, and merges the results with offsets shifted by chunk start.

### 5.3 Locating issues

For each returned issue:
1. Find all occurrences of `text` in the snapshot. Exactly one: use it.
2. Several: pick the one whose neighbouring characters best match `context_before` / `context_after`. If still tied, take the first one not already claimed by another issue.
3. None: try a case-insensitive and whitespace-normalised match. If that fails, drop the issue and count it in the status bar ("1 issue could not be placed").
4. Overlapping issues: keep the earlier one, drop the later one.

Go strings are UTF-8 but Qt text positions are UTF-16 code units. The locator works on the Go string and converts byte offsets to UTF-16 offsets once, at the boundary to the UI. This conversion has its own tests with emoji and accented characters.

### 5.4 Explanation request

When the user selects an issue, a second request is sent with: the full text (or the surrounding paragraph for long texts), the issue's `text`, `category` and `message`. The system prompt asks for a markdown answer with these sections:

1. **What is wrong:** the rule in plain language.
2. **Why:** a short explanation of the reasoning.
3. **Better ways:** two or three corrected or alternative versions, each as a list item.
4. **Remember:** one memorable tip or a similar example.

The response is streamed through langchaingo's streaming callback and appended to the panel. The request is cancelled if the user selects another issue or edits the text such that the issue is invalidated.

### 5.5 Edits after a check

The editor listens to `contentsChange(position, removed, added)`.
- Edits fully before an issue shift its range by the net length change.
- Edits that touch or fall inside an issue remove that issue's underline and its list row immediately.
- Edits after an issue leave it untouched.
- A result arriving for an older revision is rebased across the recorded edits, or discarded if rebasing is not possible.

Auto-check is optional and off by default (stored as `auto_check = false` when the setting is missing). With it off, no check ever runs except when the user presses Check. With auto-check on, a debounce timer restarts on every edit and triggers a check when the user has paused for the configured delay.

### 5.6 Error type taxonomy

Counting only makes sense if the same mistake always gets the same name. LLMs invent inconsistent labels when left free, so the types are a fixed list in code (`internal/analysis`), each with an `id`, a display `label` and a `category`. The check prompt includes the list and tells the model to pick exactly one id.

| Category | Error types (id) |
|---|---|
| grammar | `subject_verb_agreement`, `verb_tense`, `verb_form`, `modal_verb`, `article`, `preposition`, `plural_singular`, `countable_uncountable`, `pronoun`, `comparative_superlative`, `grammar_other` |
| syntax | `word_order`, `missing_word`, `extra_word`, `sentence_fragment`, `run_on_sentence`, `conditional_structure`, `syntax_other` |
| vocabulary | `wrong_word`, `confused_words`, `collocation`, `spelling`, `vocabulary_other` |

- An `error_type` that is not in the list is stored as the `_other` id of the category the model's wording best suggests. If no category can be derived, the issue is stored as `grammar_other`. This is not treated as an error.
- Punctuation and capitalisation mistakes are reported as `syntax_other`.
- The list is versioned with the code. Ids are stable, so statistics survive label changes. Adding an id is a code change; the migrations do not need to change because the database stores the id as text.

### 5.7 Counting mistakes (statistics)

- A **mistake** is one placed issue found by a check. Each one is stored as an event with its error type, category, the wrong snippet and a timestamp.
- **When it is recorded:** only when the user presses Check, after the issues have been placed. Auto-check results are underlined but not recorded, because text that is still being typed would produce counts for half-written sentences. If the user selects an unrecorded issue, the header count excludes it.
- **No double counting:** running Check again on the same text must not count the same mistake twice. Each event has a fingerprint: SHA-256 of the normalised snippet (lowercased, whitespace collapsed) and its occurrence index, meaning it is the n-th occurrence of that snippet in the whole checked text, counted from the start. Nothing that comes from the model's own judgement goes into the fingerprint (no error type, no context, no position), because the model can answer differently on every run. If a later check labels the same mistake with a different error type, the existing event keeps its first type. The event also has a `source_key` that identifies the text being written:
  - `doc:<id>` for a library document,
  - `file:<absolute path>` for a file,
  - `session:<uuid>` for a new unsaved text, generated at New or at start-up.

  The pair (`source_key`, `fingerprint`) is unique. Inserting an existing pair does nothing.

  **Which source a text has** follows how VS Code identifies a buffer: a text has exactly one identity at a time, and *Save As* moves it to the new identity.
  - New starts a `session:<uuid>` source. Open (file) gives `file:<path>`. Open from the library gives `doc:<id>`.
  - Save As, or Save to library on a text that already has a source, switches the source to the new place, like Save As does in VS Code. The event rows are moved to the new `source_key`; if that would create a duplicate pair, the duplicate is dropped. Nothing is copied, so the mistakes are not counted twice.
  - The source is whichever place the text was most recently opened from or saved to.

  **Known limits:** if the text is later opened from its *other* location (for example from the file after the source switched to the library), it is a new source and its mistakes are counted again. Inserting or deleting an identical snippet earlier in the text shifts the occurrence indexes of later ones. Both are accepted in v1.
- **Repeated mistakes count every time they are made.** The same error type made twice in one text counts twice, and so does the same snippet written again in a different text, because the point of the statistics is to show how often the user repeats a mistake. Only re-checking the *same* mistake in the *same* text is not counted again.
- Fixing a mistake does not remove its count: the count says how often the user made the mistake, not how many remain.
- **Not an error:** the button in the explanation panel deletes the issue's event, and the fingerprint is kept in an in-memory ignore set for the rest of the session so a re-check does not record it again. After a restart, the same false positive can be recorded again; this is accepted in v1.
- The counts shown in the panel header and the Statistics dialog are read from the database with gorm's builder (`COUNT` grouped by error type, filtered by `created_at`).

### 5.8 Token usage

- Every LLM request (each check chunk and each explanation; Test connection is excluded) records its input and output token counts.
- langchaingo returns provider usage in the response's `GenerationInfo` map, and the key names differ per provider. `internal/llm` normalises them into `Usage{InputTokens, OutputTokens, Estimated}`. The exact keys, and whether streamed responses include usage, must be checked against the pinned langchaingo version when this is implemented. If a provider reports nothing, both numbers are estimated (about 4 characters per token) and `Estimated` is true; estimated numbers are shown with a `~`.
- The status bar shows the last request and the session totals, for example `last: 812 in / 164 out · session: 5.2k in / 1.1k out`. Session totals reset when the app starts.
- Each request is also saved in `llm_usage` (LLM name, kind `check` or `explain`, input, output, estimated flag, timestamp). The Token usage tab of the Statistics dialog sums it per LLM for all time, 30 days and 7 days.
- No money amount is calculated: prices differ per provider and change. Only tokens are shown.
- A cancelled request records the usage it reports; if it reports none, nothing is recorded.

### 5.9 Untrusted text and output

The LLMs in this app have no tools: no file, network or code access. Text in an essay that says "ignore your instructions" can therefore only change the model's answer (missing or wrong issues, a misleading explanation, junk output). It cannot touch the user's machine or data. The measures below are only to keep the answers usable:

- The prompt puts the user's text between delimiter lines that contain a random per-request token (so the text cannot contain the closing delimiter by accident), and tells the model that everything between them is text to analyse, never instructions.
- Output is validated before it is used: JSON must match the contract, snippets that are not in the text are dropped, and unknown error types become `_other`.
- The explanation is untrusted markdown. Remote images and other remote content are never fetched, and clicking a link (http/https only) first shows a confirmation dialog with the full URL.

## 6. Concurrency

- All Qt calls happen on the UI thread. Background work (LLM calls, database access) runs in goroutines and posts results back using miqt's main-thread dispatch helper.
- Every goroutine that does I/O receives a `context.Context`. Closing the window cancels the root context and waits for goroutines to end.
- The check and explanation requests have timeouts (default 60 s and 120 s). A timeout produces the same failure path as any other LLM error.

## 7. Saving text

Two independent mechanisms:

**Files**
- *New* clears the editor (asks first if there are unsaved changes).
- *Open* loads a `.txt` or `.md` file as UTF-8. Invalid UTF-8 is rejected with a message.
- *Save* writes to the current path, or behaves like *Save As* if there is none.
- *Save As* opens a file dialog (`.txt`, `.md`).
- The window title shows the file name and a `*` for unsaved changes. Closing with unsaved changes asks to save, discard or cancel.

**Library (SQLite)**
- *Save to library* stores the current text as a document (title, content, timestamps). The title defaults to the first line, shortened to 60 characters, and can be edited.
- A *Library* dialog lists saved documents sorted by last update, with Open, Rename and Delete. Opening a document loads it into the editor, which then tracks the library document it came from; the next *Save to library* updates that row instead of creating a new one.
- Issues (underlines) are not stored, so a loaded text must be checked again. Recorded mistakes (section 5.7) are stored; checking a loaded text again does not count them twice.

## 8. Data storage

Location: `~/.english-tutor/` (created with mode `0700`). The database file is `~/.english-tutor/english-tutor.db`, created and kept at mode `0600`. The home directory is resolved with `os.UserHomeDir`.

Tables (created by numbered migrations, `001_init` first):

| Table | Columns |
|---|---|
| `llm_configs` | `id`, `name` (unique), `provider`, `model`, `base_url`, `api_key`, `temperature`, `created_at`, `updated_at` |
| `documents` | `id`, `title`, `content`, `created_at`, `updated_at` |
| `error_events` | `id`, `error_type` (taxonomy id, text), `category`, `snippet`, `source_key`, `fingerprint`, `llm_name`, `created_at`; unique index on (`source_key`, `fingerprint`); index on (`error_type`, `created_at`) |
| `llm_usage` | `id`, `llm_name`, `kind` (`check` or `explain`), `input_tokens`, `output_tokens`, `estimated`, `created_at` |
| `settings` | `key` (primary key), `value` |

`error_events` is created by migration `002_error_events` and `llm_usage` by `003_llm_usage`.

Settings keys: `selected_llm_id`, `auto_check`, `auto_check_delay_s`, `font_size`, `splitter_sizes`, `window_geometry`.

API keys are stored in plain text in the database file. The `0600` permissions are the only protection. The Preferences dialog states this next to the API key field. Keys are never written to logs, error metadata or error messages.

**Logs** are written to `~/.english-tutor/logs/english-tutor.log` (directory `0700`, file `0600`) using the standard library `log/slog`. The file is rotated by size (5 MB, 3 old files kept) by a small handler in `internal/logging`, without a new dependency. Errors are logged as `appErr.ToJson()`. API keys, the user's text and the LLM's replies are never logged. Token counts and timings are. If the log file cannot be opened, the app logs to stderr and continues.

A `Store` type wraps `*gorm.DB` and is constructed with an open database, so tests use an in-memory SQLite and production code uses the file. There is no package-level database variable.

## 9. Package layout

```
cmd/english-tutor/        main.go: wiring only
internal/config/          data directory and path resolution
internal/logging/         slog setup, size-rotated log file in ~/.english-tutor/logs
internal/store/           gorm models, numbered migrations, Store
internal/llm/             Provider enum, ModelConfig -> langchaingo llms.Model factory, token usage extraction
internal/analysis/        prompts, response parsing, issue locating, chunking, error type taxonomy
internal/ui/              main window, editor, issues list, explanation panel, dialogs
internal/app/             application service tying store, llm and analysis to the UI
```

- `internal/analysis` and `internal/llm` do not import miqt, so they can be tested without a display.
- The UI depends on small interfaces (`Analyzer`, `Explainer`, `ConfigStore`, `DocumentStore`) implemented by `internal/app`, so UI code can be driven by fakes.

## 10. Error handling

All errors use `github.com/rm4n0s/errors` as described in CLAUDE.md. Each failure below has its own unique Tag and a test that asserts the route.

| Tag | Where | When |
|---|---|---|
| `DataDirCreateFailed` | `config` | `~/.english-tutor` cannot be created |
| `HomeDirNotFound` | `config` | the home directory cannot be resolved |
| `DatabaseOpenFailed` | `store` | the SQLite file cannot be opened |
| `MigrationFailed` | `store` | a numbered migration fails |
| `LLMConfigNameTaken` | `store` | duplicate LLM name |
| `LLMConfigInvalid` | `store`/`llm` | missing model, base URL or key for the provider |
| `LLMConfigNotFound` | `store` | selected LLM id does not exist |
| `DocumentNotFound` | `store` | library document id does not exist |
| `ErrorEventSaveFailed` | `store` | recording mistakes after a check fails (the underlines are still shown) |
| `StatsQueryFailed` | `store` | reading counts for the panel header or the Statistics dialog fails |
| `StatsResetFailed` | `store` | Reset statistics or Not an error fails to delete |
| `UsageSaveFailed` | `store` | saving a request's token usage fails (the check result is still shown) |
| `LogFileOpenFailed` | `logging` | the log file cannot be opened (the app falls back to stderr) |
| `UnknownProvider` | `llm` | provider string is not supported |
| `LLMClientCreateFailed` | `llm` | langchaingo client construction fails |
| `LLMCallFailed` | `analysis` | the request fails (network, auth, rate limit) |
| `LLMCallTimeout` | `analysis` | the request exceeds its timeout |
| `LLMResponseInvalid` | `analysis` | the reply is not valid JSON for the contract, even after one retry |
| `FileReadFailed` / `FileWriteFailed` | `app` | file open or save fails |
| `FileNotUTF8` | `app` | opened file is not valid UTF-8 |

- The check prompt is retried once with a "return valid JSON only" reminder before `LLMResponseInvalid` is returned. Code fences around the JSON are stripped before parsing.
- The UI shows the `Message` of an error to the user in a non-modal banner and logs `appErr.ToJson()`.
- Cancelling is not an error (section 5.1). A cancelled context is recognised before any error tag is created, so it produces no banner and no log entry above info level.

## 11. Testing

- Table-driven unit tests throughout, next to the code.
- **store:** in-memory SQLite; migrations run from scratch; CRUD for configs and documents; uniqueness and not-found tags.
- **llm:** factory returns the right client per provider and rejects invalid configs; no network.
- **analysis:**
  - Parsing of valid, fenced, truncated and wrong-schema replies.
  - Locator cases: unique match, repeated text with context, no match, overlapping issues, case and whitespace differences, non-ASCII text and UTF-16 offset conversion.
  - Chunking at paragraph boundaries.
  - Taxonomy: every id has a category and a label; unknown ids map to the right `_other` id.
  - Fingerprint: stable under case and whitespace changes, different for different types or contexts.
- **statistics:** in-memory SQLite. Recording the same check twice counts once; the same snippet in two sources counts twice; events move correctly when a session text is saved to a file or the library, including the duplicate case; grouped counts and the 7 / 30 day windows (with a fake clock); Not an error deletes the event and the ignore set stops re-recording; Reset deletes all events.
  - A fuzz test for the locator and the offset conversion (any input must not panic and must return in-range offsets).
- **LLM calls:** a fake LLM (`httptest` server speaking the OpenAI-compatible wire format, or a fake `llms.Model`) that returns canned replies, errors, delays and streamed chunks. No real provider is ever called in tests.
- **UI:** the logic that does not need a display (edit-rebasing of issues, the shared issue state with its selection used by the editor and the list, category filtering, list row text and ordering, state machine of the explanation panel, debounce) lives in plain Go types and is unit tested. A small manual smoke checklist covers drawing and dialogs.
- **Counting:** a second check where the fake LLM returns different context or a different error type for the same snippet records nothing new; two identical snippets in one text count twice; Save As moves the events to the new source without duplicates.
- **Cancel:** cancelling a check leaves underlines and events unchanged and produces no error; cancelling mid-chunk discards finished chunks; a timeout still returns `LLMCallTimeout`.
- **Token usage:** each provider's `GenerationInfo` shape is normalised correctly (using recorded sample responses); a missing usage gives an estimate with the flag set; session totals and the 7 / 30 day sums are right.
- **Untrusted text:** a text containing the closing delimiter or "ignore previous instructions" still produces a prompt with a unique random delimiter and the same parsing path; links other than http/https are never opened.
- **Logging:** the log file is created with `0600`, rotates at the size limit, and contains no API key or user text.
- Every tag in section 10 has a test that asserts the route with `HasRoute`.
- Definition of done per CLAUDE.md: `gofmt`, `go vet ./...`, `golangci-lint run`, `go test -race ./...`. Tests that need a display are skipped when none is available.

## 12. Milestones

1. **Skeleton:** module, `config`, `logging`, `store` with `001_init`, wiring in `main.go`, empty main window.
2. **Preferences:** LLM list CRUD dialog, selector in the toolbar, persisted selection, Test connection.
3. **Analysis core:** `llm` factory, check prompt, parsing, locator, chunking (no UI), with tests.
4. **Editor integration:** Check and Cancel buttons, coloured underlines with the legend, issues list with the shared selection, tooltips, issue navigation, edit rebasing.
5. **Explanation panel:** streamed markdown, states, cache, Regenerate.
6. **Statistics and usage:** taxonomy, `002_error_events` and `003_llm_usage` migrations, recording on Check with de-duplication, panel header counts, Not an error, token usage in the status bar, Statistics dialog with the Token usage tab.
7. **Saving:** file New/Open/Save/Save As, library dialog, unsaved-changes prompts, moving session events to the saved source.
8. **Polish:** auto-check, settings persistence, status bar, README with build steps.

## 13. Risks and open points

- **LLM quality varies:** small local models may return invalid JSON or invent errors. Mitigated by the retry, the strict locator (invented text that is not in the document is dropped) and the user choosing the model.
- **Statistics accuracy:** counts are only as good as the LLM. Different models may classify the same mistake differently, and false positives inflate counts. Mitigated by the fixed taxonomy, de-duplication and the Not an error button; switching models mid-way can still shift the counts between types.
- **Qt 6 packaging:** building miqt needs Qt dev packages and a long first compile. Document it; no cross-platform packaging in v1.
- **Plain-text API keys:** accepted for v1 (section 8). An OS keyring is a possible later change.
- **Privacy:** the text is sent to the selected provider. The Preferences dialog should say so for non-local providers.
- **Not decided yet:** language of the explanations (English only in v1; a "explain in my language" preference is a candidate for later), a retention limit for the library, and whether the category colours should become configurable.
