# English Tutor: Specification

Status: draft v0.1

## 1. Purpose

A desktop application for practising written English. The user writes a sentence or an essay in a text editor. An LLM finds grammar, vocabulary and syntax errors, and the editor underlines them, with a different colour and line style for grammar, vocabulary and syntax errors. When the user selects an error, a side panel explains why it is wrong and shows better alternatives, rendered from the LLM's markdown.

## 2. Goals and non-goals

**Goals**
- Write English text in a plain editor with LLM-detected errors underlined, coloured by category.
- Learn from each error: a markdown explanation and better alternatives, next to the editor.
- Ask for other ways to write any selected word, sentence or paragraph, from the editor's right-click menu (sections 4.2 and 5.10).
- Choose which LLM is used from a selector above the editor.
- Manage the LLM list in a preferences dialog, stored in SQLite.
- Save the text to a file and to an in-app document list.
- Score the user by error type: count how many times each kind of mistake was made, so the user sees which mistakes they repeat.

**Non-goals (v1)**
- No rich text formatting; the editor is plain text.
- No offline/rule-based checker (LanguageTool etc.); every error comes from the LLM.
- No languages other than English as the text being checked.
- No accounts, sync or cloud storage.
- No mobile app and no hosted service. The browser version (section 4.7) shows the same window to the user's own browser, from the user's own computer.
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
| Web server | standard library (`net/http`, `html/template`) and the Datastar client, a vendored JavaScript file (MIT) |

Dependencies and constraints:
- miqt needs cgo and a C++ toolchain with the Qt 6 development packages installed, so the build is not pure Go. Build instructions go in the README.
- The gorm SQLite driver is `gorm.io/driver/sqlite` (cgo), approved by the user because cgo is already required for Qt. A pure-Go driver would be the alternative if cgo ever has to go.
- CLAUDE.md requires approval before adding dependencies beyond those named by the user. Each of the gorm driver, any markdown helper and any test helper needs approval when it is first added.
- All database access goes through gorm's builder. The schema is created by numbered Go migrations in `internal/store`. No hand-written SQL.

## 4. User interface

### 4.1 Main window

```
+----------------------------------------------------------------------+
| LLM: [ Claude Sonnet v ]  [Check]  [ ] Auto-check  [Stats] [Prefs]   |
| [New] [Open] [Save] [Save As] [Save to library] [Library]            |
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
- **Toolbar, row 1:** LLM selector (`QComboBox`), Check button, Auto-check checkbox (optional, **off by default**; the Check button always works), Statistics button (section 4.6), Preferences button, and the **Start web server** button, which becomes **Stop web server** while the server runs (section 4.7). While a check is running, the Check button turns into a **Cancel** button (section 5.1).
- **Toolbar, row 2:** New, Open, Save, Save As, Save to library, and Library (section 7).
- **Keyboard:** `Ctrl+Return` presses Check (or Cancel while a check runs), `Ctrl+N`, `Ctrl+O`, `Ctrl+S` and `Ctrl+Shift+S` press New, Open, Save and Save As, and `F7` / `Shift+F7` move between issues (section 4.2). The tooltips of the buttons name their shortcut.
- The LLM selector lists the configured LLMs by name. The last selection is remembered. If the list is empty, the selector is disabled, Check is disabled, and the explanation panel shows a markdown message that links to Preferences.
- The status bar shows the issue count, the check state (idle, checking, cancelled, failed), the word count, and the input / output tokens of the last request and of the session (section 5.8).

- **System tray:** when the desktop has a system tray, the application lives in it. Closing the window (the X button) only hides it: the text, a running check and the auto-check timer are left as they are, and the window state is saved. The tray icon has a menu with **Open**, which shows the same window again (restoring it if minimised), **Start web server** (**Stop web server** while it runs), **Open web page** (enabled while it runs; section 4.7), and **Close**, which quits the application; clicking the icon also opens the window. Quitting asks about unsaved changes first (Save, Discard, Cancel), with the window brought to the front for the question; Cancel keeps the application running. If the desktop has no system tray, nothing is hidden: closing the window asks about unsaved changes and quits, as it would without this feature.

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
- Keyboard: `F7` / `Shift+F7` jump to the next / previous issue: from the selected issue, or with nothing selected from the text cursor (the first issue that starts after it, or before it for `Shift+F7`), wrapping around at the ends. Hidden categories are skipped. The text cursor is put at the start of the issue.
- The tooltip reads `<error type label> · <Category>`, then the issue's short message on the next line (for example "Subject-verb agreement · Grammar" and "Use goes."). A hidden category has no tooltip. The position under the mouse is the nearest cursor position, so pointing at the right half of an issue's last character does not show its tooltip.
- The cursor moving into an issue (by click or keys) selects it without moving the cursor; selecting an issue from elsewhere (the list, F7) moves the cursor to its start without taking keyboard focus from the widget that has it.
- Editing invalidates issues (section 5.5).
- **Right-click menu.** The editor shows Qt's standard menu (Undo, Redo, Cut, Copy, Paste, Delete, Select All), and when text is selected it adds one more entry at the end, **Alternative**. It is not offered with no selection, or with a selection of only white space or line breaks. Choosing it opens the Alternatives window (below) for the selection, which can be a word or several paragraphs (at most 600 words, section 5.10). The selection is read when the entry is chosen, in UTF-16 units like every other editor position.
- **Alternatives window.** A `QDialog` that is not modal, so the user can go on working in the main window while it is open. It shows the selected text (cut to 400 characters if longer), a line with the state, and a list with one row per alternative, in the order the model gave them. States: *loading* ("Looking for alternatives..." and a busy indicator), *ready* ("3 alternatives", the first row selected; "No alternatives found." for an empty list) and *failed* (the message of the error in red, "Could not get alternatives." and a **Retry** button). **Double-clicking a row uses it**: the alternative replaces the selected text in the editor, as one step of the undo history, the cursor goes after it, the window closes and the editor gets the keyboard focus. The text is replaced only if the words at the place of the selection are still the ones that were selected; if the user has typed in the meantime so that they moved or changed, nothing is replaced and the window says so under the list, with the alternatives still there. The edit is reported like typing, so underlines it touches go (section 5.5). **Copy** puts the current row on the clipboard instead; **Close** closes the window, and closing it by any means cancels a request that still runs, without an error. Only one window is open at a time: choosing Alternative again replaces it, and the answer of the earlier request is dropped. The alternatives are untrusted LLM output and are shown as plain text, never as markdown or HTML (section 5.9). The window changes the text of the editor only by a double click on a row, and never the statistics.

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
- The count badge is hidden for issues of an auto-check (they are not recorded). A selected row has a pale background and a bar on its left, so the coloured marker and the grey second line stay readable.
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
- Details of the implementation:
  - The button next to the count is **Regenerate** when the explanation is ready or was cancelled, and **Retry** when it failed; both ask the LLM again. **Cancel** is shown only while loading. **Not an error** is shown in every state except idle. After Cancel the body says "Explanation cancelled." and the Regenerate button stays.
  - The header numbers are read from the statistics when the issue is selected. For an issue found by auto-check (not recorded) the header ends with "This one is not counted: only the Check button counts."; with no recorded mistakes of the type it reads "<label>: no mistake of this type recorded yet".
  - A streaming explanation is re-rendered at most every 100 ms (the first chunk at once). The view keeps following the end of the text unless the user has scrolled up.
  - Selecting nothing (clicking elsewhere), removing the selected issue (an edit or Not an error) or a new check result cancels a running explanation and returns the panel to idle; a result that arrives late is ignored. A new check result also clears the explanation cache.
  - If no LLM is configured any more when an issue is selected, the panel shows the failed state with that message.
  - **Links.** Qt never opens a link or loads a resource by itself. A clicked link goes through `DecideLink`: `http` and `https` links with a host get a confirmation dialog showing the full address and are then opened in the system browser; every other scheme (`file:`, `javascript:`, `data:`, `ftp:`, `mailto:` and relative links) is ignored; the `preferences:` link works only in the idle hint, never inside an explanation. The browser's resource loader returns nothing, so an image written into an explanation (remote or `file:`) is not fetched or read.

### 4.5 Preferences dialog

- A modal `QDialog` opened from the Preferences button.
- **LLMs tab:** a list of configured LLMs with Add, Edit, Delete, and Test connection. The edit form fields:

  | Field | Notes |
  |---|---|
  | Name | Unique, shown in the selector |
  | Provider | `ollama`, `openai-compatible`, `anthropic` |
  | Model | Free text, for example `llama3.1`, `gpt-4o`, `claude-sonnet-5-5` |
  | Base URL | Required for `ollama` (default `http://localhost:11434`) and `openai-compatible`; optional for `anthropic`. Must be an `http://` or `https://` address (langchaingo's Ollama client would otherwise exit the program on an unparsable URL) |
  | API key | Required for `anthropic` and usually for `openai-compatible`; not used by `ollama`. Entered in a password field, with a Show / Hide button next to it (always masked when the form opens) |
  | Temperature | Optional, default 0.2 |

- **Test connection** sends a tiny prompt and reports success (with the time the LLM took) or the failure reason. It never blocks the UI thread: the request runs in a goroutine and its result is posted back, and it is dropped if the dialog was closed meanwhile. It is available for the selected row of the list and, using the values currently typed, in the Add / Edit form, where it does not need a name. The request is not counted in the token usage (section 5.8).
- A new configuration starts with the provider `openai-compatible` and an empty Base URL (`llm.DefaultProvider`); the user picks another provider in the form.
- The Add / Edit form is validated before it is saved (the messages come from `llm.ModelConfig.Validate`) and shows store errors such as a duplicate name without closing. Choosing `ollama` fills in `http://localhost:11434` if the Base URL is empty; switching away from `ollama` clears that default again, so an Anthropic or OpenAI-compatible configuration never inherits a localhost address. The API key field is disabled for `ollama`, and a key typed before switching to `ollama` is not stored.
- **General tab:** auto-check on/off (default off), auto-check delay in seconds (default 3, used only when auto-check is on; the field is disabled while auto-check is off), editor font size. Changes apply and are saved at once; there is no OK button, only Close.
- **Web server tab:** the state of the web server with a Start / Stop button, the port (default 3333) and the password (section 4.7). A new port or password is saved when the field is left and restarts a server that runs.
- The Add / Edit form shows a privacy note, "the text you write is sent to <server> when you check it, ask for an explanation or ask for alternatives", unless the server is on this computer (the host is `localhost`, `127.x.x.x` or `::1`, or Ollama with its default address). For Anthropic with no base URL it names Anthropic's server.
- Deleting the currently selected LLM selects another one (the first by name), or leaves the selector empty if none remain; the new choice is remembered.

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
- Details: the dialog is modal and has two tabs, **Mistakes** and **Token usage**, and a Close button. The period selector hides the error types that have no mistake in that period and drives the summary line ("24 mistakes in all time. Most frequent: Article."); the columns always show all-time, 7-day and 30-day counts. Clicking a header sorts by it: a number or date column starts with the largest or newest first, a text column alphabetically, and a second click reverses; ties are ordered by the error type's label. The examples list shows the three most recent snippets of the selected row with their dates and is empty when no row is selected (also after a filter or sort change). Reset asks for confirmation, and after it the issues list badges and the panel header counts are cleared. The Token usage table has one row per LLM with "input in / output out" for all time, 30 days and 7 days, `~` marks counts that include estimates, and its own Reset button; resetting one tab leaves the other alone. The queries are small grouped counts and run on the UI thread when the dialog opens or after a reset. A failed query or reset shows its message in a banner at the bottom of the dialog and is logged.

### 4.7 Web server

The application can show its window in a web browser, with the same functions: the editor with the underlines, the issues list, the explanations, the library, the statistics and the preferences. The server is **off** until the user starts it, and is only started from the desktop application.

**Start and stop.** The **Start web server** button of the window, the tray item of the same name and the button on the Web server tab of Preferences start it; the same controls stop it (their text follows the state, and the tray's **Open web page** item, enabled while the server runs, opens the page in the browser). Quitting the application stops it and ends the pages in the browsers; hiding the window to the tray does not. A server that cannot start, because the port is in use for example, shows the reason in the window's banner and next to the tray icon. The server is not started at launch: whoever starts the application decides each time.

**Port.** 3333 by default, any port from 1 to 65535 on the Web server tab (`web_port`). Changing the port or the password restarts a server that runs.

**Who may connect.** Both rules apply to every request:
- *This computer* is served without a password. A request counts as coming from this computer when its source address is a loopback address *and* its `Host` header names `localhost`, `*.localhost`, `127.x.x.x` or `[::1]`. The second part keeps another website from reaching the server through the visitor's browser by pointing its own name at 127.0.0.1 (DNS rebinding).
- *Other computers* are served only if a password is set (Web server tab, at least 8 characters, kept as a salted PBKDF2-SHA256 hash in `web_password`, never as text). Without one the server listens on the loopback addresses only, so other computers cannot even connect; with one it listens on every address, and a browser from another computer gets a sign-in page. A correct password gives a session cookie (`HttpOnly`, `SameSite=Strict`, a day without requests). Five wrong passwords from one address block that address for five minutes. The page is plain HTTP, so on a network that is not trusted the password can be read; the tab says so.
- A request that changes something must come from the page itself: it carries Datastar's own header, its `Origin` is the server's own address and its `Sec-Fetch-Site` is not cross-site. Another website cannot make the browser do something to the server.
- `X-Forwarded-For` and similar headers are not trusted. A reverse proxy on this computer makes every request look local, so one must not be put in front of a server without a password.
- Every response has a strict `Content-Security-Policy` (own files only; `unsafe-eval` for Datastar's expressions; no inline script or style), `X-Frame-Options: DENY`, `nosniff` and `no-referrer`.

**Log.** Every request is logged, allowed or not, with the method, the route, the source address, the status, the size, the time and the verdict (`access=allowed`, or `access=denied` with the reason, as a warning). Query strings, bodies, workspace addresses and passwords are never logged: they hold the user's text and the key to their page (section 8).

**Pages.** The server keeps the state of every open browser tab, the way `App` keeps the state of the window: the text, the issues and the selection, the explanation, the source of the text (section 5.7), the token totals, the dialog that is open. A page is addressed as `/w/<id>/`, where the id cannot be guessed, and belongs to the browser that made it. Reloading the page, or opening it again later, finds the same state; the state is dropped after 12 hours without a browser, when the server stops or restarts, and at most 64 pages are kept. The page is built with [Datastar](https://data-star.dev): the server renders each part of it (toolbar, underline layer, issues, explanation, status bar, dialogs) as HTML and pushes only the parts that changed over one Server-Sent Events stream per tab; an action is a POST that carries the editor's text and gets `204`. A browser that connects again (a hidden tab that comes back) gets the whole page again, except the text of its own editor. A streaming explanation is sent about ten times a second (section 4.4).

**Same as the window.** The check, auto-check, cancel, the underlines (colour and line style per category, drawn under a transparent editor, so the editor is a plain `<textarea>`), the shared selection, F7 / Shift+F7, `Ctrl+Enter`, `Ctrl+S`, hover tooltips, the legend toggles, the issues list, the explanation panel with its states and header counts, Not an error, Regenerate, the statistics and token usage, the library, the LLM list (with Test connection) and the general settings. Edits move or remove underlines the way section 5.5 says: the page sends the whole text, and the server finds the edit as the part between the common start and end of the old and new text. Mistakes are recorded under the text's own source and counted once, as in section 5.7, whichever of the window and the pages checked the text; the LLM list, the selected LLM and the general settings are the same ones the window uses, and a change made in a page reloads them in the window.

**What differs, because a browser is not a desktop:**
- *Open* is the browser's file chooser; the file is uploaded, read with the rules of section 7 (UTF-8, no NUL byte, at most 10 MB, the file's line ending remembered) and shown. A browser does not tell the server where a file lives, so an opened text starts a new `session:` source instead of a `file:` one.
- *Save* and *Save As* download the text as a file; the browser decides where it goes. Save uses the name of the text (a text without one asks for it, proposing its first line, and `.txt` is added when the name has no extension); Save As asks every time. The file is sent once from an address only that page knows and is not kept.
- Before New, Open and opening a library document, a text with unsaved changes asks `Discard them?` in the browser; there is no Save choice in that question.
- Right-click menus are replaced by the buttons of the explanation panel, `Ctrl+N` and `Ctrl+O` are left to the browser. The Alternatives window (section 4.2) is not part of the browser version in v1.
- API keys are never sent to the browser. The edit form of an LLM says that a key is stored and keeps it when the field is left empty; a box removes it. The port and the password are set only in the window.
- The explanation is untrusted markdown (section 5.9). A small renderer in `internal/web` turns it into HTML: it escapes everything first, shows raw HTML and images as text, and makes a link only for `http` and `https` addresses with a host, opened in a new tab (`noopener`) after a confirmation that shows the whole address. It is fuzzed to write no tag but its own.

## 5. Error analysis

### 5.1 Pipeline

1. The user presses Check (or auto-check fires).
2. The app snapshots the editor text and a revision counter, and sends the check prompt to the selected LLM.
3. The LLM replies with a JSON list of issues.
4. The app validates the JSON, finds each issue's position in the snapshot text (section 5.3), and drops issues it cannot place.
5. If the document is still at the same revision, the UI replaces the underlines. If the user kept typing, the result is discarded (or rebased, see section 5.5).

Only one check runs at a time. Starting a new check cancels the previous one through its `context.Context`.

A check started with the Check button is *manual* and records its mistakes (section 5.7); one started by auto-check is not recorded. When auto-check is on, the debounce timer restarts on every edit; when it fires while a check is still running it waits for another pause instead of cancelling that check. The status bar shows `Ready`, `Checking...`, `Check cancelled`, `Check failed`, or after a check `N issues` / `No errors found`, with ` · N issues could not be placed` appended when the locator dropped some. A failed check shows its message in a dismissible banner above the editor (cleared when the next check starts) and keeps the old underlines. The "last request" tokens in the status bar are those of the whole check (all its chunks and retries); the session total adds them up.

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

Parsing is tolerant about packaging and strict about content: markdown fences and prose around the JSON are ignored, and a reply that is just a JSON array of issues is accepted, but the JSON must contain an `issues` list (an object without it, `null` or text is invalid). Entries without `text` are dropped and counted. `message` is cut at 300 characters, the contexts at 200, and a snippet over 2000 characters is dropped, so a model that ignores the contract cannot flood the UI. An empty `message` falls back to the label of the error type. The reply is never put in error metadata or logs.

The prompt and the response structs live in `internal/analysis`. For long texts the app splits the input into chunks (at most 2000 words per request) with `analysis.SplitChunks`: a chunk ends at a paragraph boundary (blank line) where possible; a paragraph over the limit is cut between sentences, and a sentence over the limit between words. The chunks are contiguous and cover the whole text, whitespace stays with the text before it, and a text with no words gives no chunks. The app runs the chunks sequentially, locates each chunk's issues in the chunk's own text, and merges the results with offsets shifted by the chunk start (`LocateResult.Append`).

### 5.3 Locating issues

For each returned issue:
1. Find all occurrences of `text` in the snapshot. Exactly one: use it.
2. Several: prefer occurrences that do not start or end inside a word (so `go` does not match inside `good`), then the one whose neighbouring characters best match `context_before` / `context_after` (whitespace at the edge of the contexts is ignored). If still tied, take the first one not already claimed by another issue. Occurrences may overlap each other (`aa` occurs twice in `aaa`).
3. None: try a case-insensitive match that ignores differences in whitespace (any run counts as one space) and treats typographic quotes (`‘ ’ “ ”`) and dashes (`– — −`) as the plain ones, since models often normalise them. If that fails, drop the issue and count it in the status bar ("1 issue could not be placed").
4. Overlapping issues: keep the earlier one, drop the later one.

Go strings are UTF-8 but Qt text positions are UTF-16 code units. The locator works on the Go string and converts byte offsets to UTF-16 offsets once, at the boundary to the UI, with `analysis.UTF16Map` (a runes above U+FFFF count as two units, an invalid byte as one). This conversion has its own tests with emoji and accented characters, and a fuzz test against `unicode/utf16`.

### 5.4 Explanation request

When the user selects an issue, a second request is sent with: the full text (or the surrounding paragraph for long texts), the issue's `text`, `category` and `message`. The system prompt asks for a markdown answer with these sections:

1. **What is wrong:** the rule in plain language.
2. **Why:** a short explanation of the reasoning.
3. **Better ways:** two or three corrected or alternative versions, each as a list item.
4. **Remember:** one memorable tip or a similar example.

The response is streamed through langchaingo's streaming callback and appended to the panel. The request is cancelled if the user selects another issue or edits the text such that the issue is invalidated.

Details of `analysis.Explainer`: the issue's snippet is taken from the checked text at the issue's offsets (not from the model's quote). The context sent is the whole text if it has at most 600 words, otherwise the paragraph around the issue, cut to a 600-word chunk if the paragraph itself is longer. The snippet and context are delimited like in the check prompt. Only one explanation runs at a time: starting one, or reading a cached one, cancels the one that is streaming. Explanations are cached by a key the caller chooses (the UI uses the check id and issue id) and `Reset` clears the cache when a new check result replaces the old one. An empty answer is `LLMResponseInvalid`.

### 5.5 Edits after a check

The editor listens to `contentsChange(position, removed, added)`.
- Edits fully before an issue shift its range by the net length change.
- Edits that touch or fall inside an issue remove that issue's underline and its list row immediately.
- Edits after an issue leave it untouched.
- "Touch" includes adjacent edits: an edit whose range ends at or after the issue's start and starts at or before its end removes the issue (typing right after a word changes that word). Only edits that end strictly before the issue's start shift it, and only edits that start strictly after its end leave it alone. The editor reports UTF-16 positions, and `internal/issues` works in UTF-16 units throughout (`issues.FromPlaced` converts the analysis package's byte offsets once).
- The edits made while a check runs are logged (at most 100,000) with the revision the check started at. When the result arrives, its issues are rebased across that log in order, and the ones an edit touched are dropped. A result of a check that is no longer the pending one is ignored. If the log overflowed, the result is discarded and the old issues stay. Aborting a check (cancel or failure) leaves the old issues untouched.
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

  The pair (`source_key`, `fingerprint`) is unique. Inserting an existing pair does nothing. The occurrence index is the number of times the same snippet (ignoring case) occurs in the text before it (`analysis.Fingerprint`).

  **Which source a text has** follows how VS Code identifies a buffer: a text has exactly one identity at a time, and *Save As* moves it to the new identity.
  - New starts a `session:<uuid>` source. Open (file) gives `file:<path>`. Open from the library gives `doc:<id>`.
  - Save As, or Save to library on a text that already has a source, switches the source to the new place, like Save As does in VS Code. The event rows are moved to the new `source_key`; if that would create a duplicate pair, the duplicate is dropped. Nothing is copied, so the mistakes are not counted twice.
  - The source is whichever place the text was most recently opened from or saved to.

  **Known limits:** if the text is later opened from its *other* location (for example from the file after the source switched to the library), it is a new source and its mistakes are counted again. Inserting or deleting an identical snippet earlier in the text shifts the occurrence indexes of later ones. Both are accepted in v1.
- **Repeated mistakes count every time they are made.** The same error type made twice in one text counts twice, and so does the same snippet written again in a different text, because the point of the statistics is to show how often the user repeats a mistake. Only re-checking the *same* mistake in the *same* text is not counted again.
- Fixing a mistake does not remove its count: the count says how often the user made the mistake, not how many remain.
- **Not an error:** the button in the explanation panel deletes the issue's event, and the fingerprint is kept in an in-memory ignore set for the rest of the session so a re-check does not record it again. After a restart, the same false positive can be recorded again; this is accepted in v1.
- The counts shown in the panel header and the Statistics dialog are read from the database with gorm's builder (`COUNT` grouped by error type, filtered by `created_at`).
- `store.Stats` is the API for all of this: `Record` (reports inserted, duplicate and ignored counts), `NotAnError`, `MoveSource`, `TypeStats` (one row per error type with all-time, 30-day and 7-day counts and the last time seen), `CountForType` (panel header), `RecentExamples`, `ResetMistakes`, and `Summarize` for the line above the table. A window is "from now minus 7 (or 30) days, inclusive". `MoveSource` failing is `ErrorEventSaveFailed`. Time comes from an injected clock so the windows are testable.

### 5.8 Token usage

- Every LLM request (each check chunk, each explanation and each alternatives request; Test connection is excluded) records its input and output token counts.
- langchaingo returns provider usage in the response's `GenerationInfo` map, and the key names differ per provider. `internal/llm` normalises them into `Usage{InputTokens, OutputTokens, Estimated}`. With langchaingo v0.1.15 the keys are `PromptTokens` / `CompletionTokens` (OpenAI-compatible and Ollama) and `InputTokens` / `OutputTokens` (Anthropic); a test runs the real clients against local servers to guard them. Streamed responses use the same extraction; a provider that sends no usage while streaming (or only zeros) falls under the next rule, and this has not been checked against real servers. If a provider reports nothing, both numbers are estimated (about 4 characters per token) and `Estimated` is true; estimated numbers are shown with a `~`.
- The status bar shows the last request and the session totals, for example `last: 812 in / 164 out · session: 5.2k in / 1.1k out`. Session totals reset when the app starts.
- Each request is also saved in `llm_usage` (LLM name, kind `check`, `explain` or `alternatives`, input, output, estimated flag, timestamp). The Token usage tab of the Statistics dialog sums it per LLM for all time, 30 days and 7 days.
- No money amount is calculated: prices differ per provider and change. Only tokens are shown.
- A cancelled request records the usage it reports; if it reports none, nothing is recorded.
- `store.Stats.RecordUsage` implements `analysis.UsageRecorder`; `UsageTotals` sums per LLM for a period (an `Estimated` flag is set when any request in the sum was estimated) and `ResetUsage` clears the table.

### 5.9 Untrusted text and output

The LLMs in this app have no tools: no file, network or code access. Text in an essay that says "ignore your instructions" can therefore only change the model's answer (missing or wrong issues, a misleading explanation, junk output). It cannot touch the user's machine or data. The measures below are only to keep the answers usable:

- The prompt puts the user's text between delimiter lines that contain a random per-request token (so the text cannot contain the closing delimiter by accident), and tells the model that everything between them is text to analyse, never instructions.
- Output is validated before it is used: JSON must match the contract, snippets that are not in the text are dropped, and unknown error types become `_other`.
- The alternatives are untrusted text too. The window shows them as plain text, so markup in them is never interpreted, and Copy and the double click use exactly what a row shows.
- The explanation is untrusted markdown. Remote images and other remote content are never fetched, and clicking a link (http/https only) first shows a confirmation dialog with the full URL.

### 5.10 Alternatives

When the user chooses **Alternative** on a selection (section 4.2), a request asks the selected LLM for other ways to write it. It is a plain request, not a stream: the answer is a short list.

- **Prompt.** A coach, not a checker: the system prompt asks for between 3 and 5 alternatives, each a replacement for the whole selected text that fits in its place (the same meaning, tone and grammatical form: a word for a word or a short phrase, a sentence or paragraph for a full rewrite), different from each other and from the selection, and correct English even when the selection has a mistake. The selection and its context are the user's text and are delimited like in the check prompt, with a random per-request token (section 5.9). The context is the whole text if it has at most 600 words, otherwise the paragraph around the selection (as in section 5.4).
- **Reply.** One JSON object, `{"alternatives": ["...", "..."]}`, parsed as tolerantly as the check reply (fences and prose around it are ignored, a bare array of strings is accepted). Entries are trimmed; empty ones, repeats and the selected text itself (ignoring case) are dropped; at most eight are kept and an entry over 6000 characters is dropped. A reply that is not valid, or has no usable alternative, is retried once with a "return valid JSON only" reminder, and then fails with `LLMResponseInvalid`. The reply is never put in error metadata or logs.
- **Limits.** A selection that is empty or only white space is `SelectionEmpty`, and one over 600 words is `SelectionTooLong` ("select at most 600"); neither reaches the model.
- **One at a time.** `analysis.Suggester` runs one request at a time: a new request, closing the window or quitting cancels the one before it. Cancelling is not an error. The timeout is 120 s, like an explanation, and gives `LLMCallTimeout`; any other failure is `LLMCallFailed`. Both are shown in the window, not in the banner, and logged.
- **Tokens.** The request, with its retry as one record, is counted in the status bar and saved with kind `alternatives` (section 5.8). Nothing is recorded in the mistake statistics.

## 6. Concurrency

- All Qt calls happen on the UI thread. Background work (LLM calls, database access) runs in goroutines and posts results back using miqt's main-thread dispatch helper.
- Every goroutine that does I/O receives a `context.Context`. Quitting the application (closing the window when there is no tray, or the tray's Close item) cancels the root context and waits for goroutines to end; hiding the window to the tray does not.
- The check and explanation requests have timeouts (default 60 s and 120 s). The check timeout applies to each chunk's request, not to the whole check. A timeout produces `LLMCallTimeout`, and any other failure `LLMCallFailed`; both show their message in the error banner. Cancelling (by the user, by a newer request or by quitting the application) is detected first and is not an error. Token usage is recorded for every request that reports it, including requests of a check that later fails or is cancelled.

## 7. Saving text

Two independent mechanisms:

**Files**
- *New* clears the editor (asks first if there are unsaved changes).
- *Open* loads a `.txt` or `.md` file as UTF-8. Invalid UTF-8 is rejected with a message.
- *Save* writes to the current path, or behaves like *Save As* if there is none.
- *Save As* opens a file dialog (`.txt`, `.md`).
- The window title shows the file name and a `*` for unsaved changes. Quitting with unsaved changes asks to save, discard or cancel (closing the window to the tray does not, section 4.1).

**Library (SQLite)**
- *Save to library* stores the current text as a document (title, content, timestamps). The title defaults to the first line, shortened to 60 characters, and can be edited.
- A *Library* dialog lists saved documents sorted by last update, with Open, Rename and Delete. Opening a document loads it into the editor, which then tracks the library document it came from; the next *Save to library* updates that row instead of creating a new one.
- Details of the implementation:
  - **Opening a file** reads it before anything is replaced, so a file that cannot be opened (not found, not valid UTF-8, binary because it contains a NUL byte, or larger than 10 MB) leaves the current text alone and shows its message in the banner; only after a successful read is the user asked about unsaved changes. A leading byte order mark is dropped. The editor works with `\n`; if a file used `\r\n` that ending is remembered and written back on Save, so a Windows file stays a Windows file.
  - **Writing** goes to a temporary file in the same folder that is renamed over the target, so a failure half way never damages the old file. An existing file keeps its permissions, a symbolic link stays a link, and a new file is `0644`. Save As adds `.txt` if the chosen name has no extension; its proposed name is the text's default title plus `.txt`.
  - **Unsaved changes** are asked about, with Save, Discard and Cancel, before New, Open, opening from the library and closing the window. Save here means: the library if the text's current source is a library document, otherwise its file (Save As if it has none); if that save fails or is cancelled the action is cancelled too. Loading a text (New, Open, library) is not typing: it clears the issues, stops a running check or explanation, and does not start an auto-check.
  - **Save to library** on a text that came from a file adds a new document (and moves the mistakes to it, section 5.7) while the text stays tied to its file, so Save still writes the file. Renaming a document in the Library updates the window title if it is the open one; deleting the open one detaches it, so the next Save to library creates a new document; saving to a document that was deleted meanwhile reports it and detaches it.
  - If moving the recorded mistakes to a new source fails (Save As), the text still switches source and the failure is shown in the banner. A mistake recorded by a check that was still being saved when Save As happened can stay under the old source.
- Issues (underlines) are not stored, so a loaded text must be checked again. Recorded mistakes (section 5.7) are stored; checking a loaded text again does not count them twice.

## 8. Data storage

Location: `~/.english-tutor/` (created with mode `0700`). The database file is `~/.english-tutor/english-tutor.db`, created and kept at mode `0600`. The home directory is resolved with `os.UserHomeDir`.

Tables (created by numbered migrations, `001_init` first):

| Table | Columns |
|---|---|
| `llm_configs` | `id`, `name` (unique), `provider`, `model`, `base_url`, `api_key`, `temperature`, `created_at`, `updated_at` |
| `documents` | `id`, `title`, `content`, `created_at`, `updated_at` |
| `error_events` | `id`, `error_type` (taxonomy id, text), `category`, `snippet`, `source_key`, `fingerprint`, `llm_name`, `created_at`; unique index on (`source_key`, `fingerprint`); index on (`error_type`, `created_at`) |
| `llm_usage` | `id`, `llm_name`, `kind` (`check`, `explain` or `alternatives`), `input_tokens`, `output_tokens`, `estimated`, `created_at` |
| `settings` | `key` (primary key), `value` |

`error_events` is created by migration `002_error_events` and `llm_usage` by `003_llm_usage`.

Settings keys: `selected_llm_id`, `auto_check`, `auto_check_delay_s`, `font_size`, `splitter_sizes`, `window_geometry`, `web_port`, `web_password`. `splitter_sizes` holds four numbers as JSON: the horizontal splitter (editor, right column) then the vertical one (issues list, explanation panel). `window_geometry` is Qt's `saveGeometry()` bytes, base64 encoded. A missing or unparsable value gives the default: `auto_check` false, `auto_check_delay_s` 3, `font_size` 13, no selected LLM, no saved splitter sizes or geometry, `web_port` 3333, no `web_password`. `web_password` holds the hash made by `web.HashPassword` (`pbkdf2-sha256$<rounds>$<salt>$<hash>`), never the password.

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
internal/issues/          issue state: placed issues, shared selection, category visibility, edit rebasing (no Qt)
internal/ui/              main window, editor, issues list, explanation panel, dialogs, tray
internal/web/             the web server: access rules, per-tab state, Datastar pages (no Qt)
internal/app/             application service tying store, llm and analysis to the UI
internal/uitest/          test harness that runs the Qt event loop for tests (QT_QPA_PLATFORM=offscreen)
```

- `internal/analysis`, `internal/issues`, `internal/llm` and `internal/web` do not import miqt, so they can be tested without a display. (`internal/web` imports `internal/ui` for its formatting and statistics helpers, which are in files without Qt, but that links Qt.)
- `internal/ui` keeps what can be tested without Qt (colours and underline styles, token and word formatting) in files that do not import miqt. `MainWindow` holds widgets and reports user actions through a `Handlers` struct; it has no application logic. `internal/app` sets the handlers and owns the persistence of window state and settings.
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
| `LLMConfigInvalid` | `store`/`llm` | missing name or model, missing or malformed base URL, missing key for the provider, temperature outside 0 to 2 (validated once, in `llm.ModelConfig.Validate`) |
| `LLMConfigNotFound` | `store` | selected LLM id does not exist |
| `DocumentNotFound` | `store` | library document id does not exist |
| `DatabaseQueryFailed` | `store` | any other query on the configs, documents or settings tables fails |
| `ErrorEventSaveFailed` | `store` | recording mistakes after a check fails (the underlines are still shown) |
| `StatsQueryFailed` | `store` | reading counts for the panel header or the Statistics dialog fails |
| `StatsResetFailed` | `store` | Reset statistics or Not an error fails to delete |
| `UsageSaveFailed` | `store` | saving a request's token usage fails (the check result is still shown) |
| `WebPortInvalid` | `store` | the web server port is not a number from 1 to 65535 |
| `WebPasswordInvalid` | `web` | the web server password has fewer than 8 characters or is too long |
| `WebServerAlreadyRunning` | `web` | the web server is started while it runs |
| `WebServerStartFailed` | `web` | the web server cannot listen on its port (in use, no permission) |
| `LogFileOpenFailed` | `logging` | the log file cannot be opened (the app falls back to stderr) |
| `LogRotateFailed` | `logging` | the log file cannot be rotated (the record is still written to the current file) |
| `UnknownProvider` | `llm` | provider string is not supported |
| `LLMClientCreateFailed` | `llm` | langchaingo client construction fails |
| `LLMCallFailed` | `analysis` | the request fails (network, auth, rate limit) |
| `LLMCallTimeout` | `analysis` | the request exceeds its timeout |
| `LLMResponseInvalid` | `analysis` | the reply is not valid JSON for the contract (or, for alternatives, has no usable alternative), even after one retry |
| `SelectionEmpty` | `analysis` | alternatives are asked for a selection that is empty or only white space |
| `SelectionTooLong` | `analysis` | the selection for alternatives has more than 600 words |
| `FileReadFailed` / `FileWriteFailed` | `app` | file open or save fails (also: file larger than 10 MB, application closing) |
| `FileNotUTF8` | `app` | opened file is not valid UTF-8, or contains a NUL byte |

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
- **Qt widgets:** tests of windows and dialogs run with `QT_QPA_PLATFORM=offscreen` through `internal/uitest` (the Qt event loop runs on the main thread and each test reaches it with `uitest.OnUI`), so no display is needed. They check widget state, handlers, persistence and the layout (including a screenshot taken during development). If the offscreen plugin is not installed they are skipped with a message.
- **UI:** the logic that does not need a display (edit-rebasing of issues, the shared issue state with its selection used by the editor and the list, category filtering, list row text and ordering, state machine of the explanation panel, debounce) lives in plain Go types and is unit tested. A small manual smoke checklist covers drawing and dialogs.
- **Counting:** a second check where the fake LLM returns different context or a different error type for the same snippet records nothing new; two identical snippets in one text count twice; Save As moves the events to the new source without duplicates.
- **Alternatives:** the prompt (delimiters with a random token, the context rule), the parser (valid, fenced, bare array, repeats, the selection itself, over-long entries, invalid) with a fuzz test, one retry on invalid JSON with both requests' usage in one record, every failure route, a selection that is empty or too long never reaching the model, cancel and replacement by a newer request; the editor menu (Alternative only with a real selection, UTF-16 positions after an emoji, a backwards selection); the window states, Copy, Retry and the double click; replacing the selection (UTF-16 positions, one undo step, the issue under it going, a text that changed meanwhile left alone); and the whole flow from the menu with a fake LLM, including closing the window mid-request.
- **Cancel:** cancelling a check leaves underlines and events unchanged and produces no error; cancelling mid-chunk discards finished chunks; a timeout still returns `LLMCallTimeout`.
- **Token usage:** each provider's `GenerationInfo` shape is normalised correctly (using recorded sample responses); a missing usage gives an estimate with the flag set; session totals and the 7 / 30 day sums are right.
- **Untrusted text:** a text containing the closing delimiter or "ignore previous instructions" still produces a prompt with a unique random delimiter and the same parsing path; links other than http/https are never opened.
- **Logging:** the log file is created with `0600`, rotates at the size limit, and contains no API key or user text.
- **Web server** (`internal/web`, with real listeners on free ports, requests handed in from chosen addresses, and a fake LLM): the access rules table (this computer, rebinding, other computers with and without a password, cross-site requests), sign-in with a wrong password, the lock after five, sessions and their expiry, the log of every request without text or addresses, one workspace per browser and no use of another's, the stream (first the whole page, then only what changed, again after a reconnect), the whole check, explanation, selection, library, files, preferences and statistics flows, API keys that never reach a page, Stop with a check running, and a fuzz test of the markdown renderer. The pages were also driven by hand in a real browser (Firefox): typing, Check, underlines under the text, the list, F7, dialogs, upload and download, light and dark, a narrow screen.
- Every tag in section 10 has a test that asserts the route with `HasRoute`.
- Definition of done per CLAUDE.md: `gofmt`, `go vet ./...`, `golangci-lint run`, `go test -race ./...`. Qt tests need Qt's offscreen platform plugin and are skipped, with a message, when it is missing.

## 12. Milestones

All of these are implemented; docs/IMPLEMENTATION.md has the 16 steps they were built in and docs/IMPLEMENTED.md the log of what each step did.

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
- **Privacy:** the text is sent to the selected provider. The LLM form says so for servers that are not on this computer (section 4.5), and the README repeats it.
- **Not decided yet:** language of the explanations (English only in v1; a "explain in my language" preference is a candidate for later), a retention limit for the library, and whether the category colours should become configurable.
