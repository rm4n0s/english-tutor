# Implementation plan

Steps for building English Tutor as described in [SPEC.md](SPEC.md). Finished steps are recorded in [IMPLEMENTED.md](IMPLEMENTED.md).

## Rules

1. **One step at a time, in the order listed.** A step starts from an up-to-date `main` (`git switch main && git pull --ff-only`).
2. **Each step has its own branch**, named `feat/<slug>` as given in the step. Never commit work for a step directly on `main`.
3. **A step is finished only when all four checks pass** on its branch:
   - `gofmt -l .` prints nothing
   - `go vet ./...`
   - `golangci-lint run`
   - `go test -race ./...`

   Anything these report is fixed. If a check cannot be run or fails, the step is not finished and nothing is published. The failure or skipped check is reported to the user.
4. **Add the step's entry to `docs/IMPLEMENTED.md` on the branch**, as the last commit of the step (format below).
5. **Publish by fast-forward, without a PR:** `git switch main && git merge --ff-only feat/<slug> && git push origin main`. If `main` has moved, rebase the branch on `main`, run the four checks again, and then merge. After publishing, delete the local branch.
6. **No `.github/` directory and no GitHub Actions.** The checks run locally.
7. **Dependencies:** only `github.com/rm4n0s/glutter`, `github.com/tmc/langchaingo`, `gorm.io/gorm` and `github.com/rm4n0s/errors` are approved. Anything else (including the gorm SQLite driver) needs the user's approval before it is added.
8. **Code conventions** are those in CLAUDE.md: errors with `github.com/rm4n0s/errors` and a route test per tag, table-driven tests, no global mutable state, `context.Context` first for I/O, no hand-written SQL, and the schema only through numbered Go migrations.
9. **The spec follows the code.** If a step shows that SPEC.md is wrong or incomplete, the fix to SPEC.md is part of that step and is mentioned in its IMPLEMENTED.md entry.
10. **Changes not listed here** (fixes after a step is published, extra features) follow the normal rule in CLAUDE.md: a `feat/...` or `fix/...` branch, and a PR instead of a direct publish.
11. Commit messages are in the imperative mood ("Add store migrations").
12. Tests of windows run on glutter's test host (`gluttertest`), which needs no display.

### Format of an IMPLEMENTED.md entry

```
## Step N: <title>
- Branch: feat/<slug>
- Date: YYYY-MM-DD
- Spec: sections covered
- Done: what was added
- Checks: gofmt, go vet, golangci-lint, go test -race (results; say if any were skipped and why)
- Notes: spec changes, decisions, known limits
```

## Steps

### Step 1: Spec and plan (`feat/docs`)
Commit `docs/SPEC.md`, `docs/IMPLEMENTATION.md` and `docs/IMPLEMENTED.md`. There is no Go code yet, so the Go checks do not apply; this is stated in the entry.

### Step 2: Project skeleton (`feat/skeleton`)
- `go.mod` (module `github.com/rm4n0s/english-tutor`), `.gitignore`, `cmd/english-tutor/main.go` kept thin.
- `internal/config`: data directory `~/.english-tutor` (0700) and path resolution, tags `HomeDirNotFound`, `DataDirCreateFailed`.
- `internal/logging`: slog to `~/.english-tutor/logs/english-tutor.log` (0600), size rotation, stderr fallback, tag `LogFileOpenFailed`.
- Spec: sections 8 (location, logs), 9, 10.

### Step 3: Store (`feat/store`)
- gorm `Store` with models and migration `001_init` (`llm_configs`, `documents`, `settings`), file mode 0600, in-memory SQLite for tests.
- CRUD for LLM configs (unique name, validation), documents, settings (typed getters with defaults, `auto_check` defaults to false).
- Needs the user's approval for the SQLite driver before starting.
- Tags: `DatabaseOpenFailed`, `MigrationFailed`, `LLMConfigNameTaken`, `LLMConfigInvalid`, `LLMConfigNotFound`, `DocumentNotFound`.
- Spec: sections 3, 8, 10.

### Step 4: LLM factory (`feat/llm`)
- Provider enum (`ollama`, `openai-compatible`, `anthropic`), config validation, factory returning a langchaingo model.
- `Usage{InputTokens, OutputTokens, Estimated}` extracted from `GenerationInfo`, with the character-based estimate as fallback. Check the real key names in the pinned langchaingo version and record them in the code comments.
- A fake model for tests (canned replies, errors, delays, streamed chunks).
- Tags: `UnknownProvider`, `LLMClientCreateFailed`.
- Spec: sections 3, 4.5, 5.8.

### Step 5: Taxonomy and response parsing (`feat/analysis-parse`)
- Error type taxonomy (ids, labels, categories, `_other` mapping).
- Check prompt with per-request random delimiter, JSON response structs, tolerant parsing (code fences stripped), single retry on invalid JSON.
- Tag: `LLMResponseInvalid`.
- Spec: sections 5.2, 5.6, 5.9.

### Step 6: Locating and chunking (`feat/analysis-locate`)
- Locator (unique match, context disambiguation, normalised fallback, overlap removal), UTF-8 to UTF-16 offset conversion, paragraph chunking.
- Fuzz tests for the locator and the offset conversion.
- Spec: sections 5.1 (chunking), 5.3.

### Step 7: Analysis service (`feat/analysis-service`)
- `Analyzer` (check with cancel and timeout, one check at a time) and `Explainer` (streamed markdown, in-memory cache, regenerate).
- Cancellation is not an error; timeout gives `LLMCallTimeout`; other failures `LLMCallFailed`.
- Usage reported per request through an interface (stored in step 9).
- Spec: sections 5.1, 5.4, 6, 10.

### Step 8: Issue state and edit rebasing (`feat/issue-state`)
- Plain Go issue state: placed issues, the shared selection, category visibility, ordering, row text, edit rebasing (`contentsChange`), revision handling.
- No Qt imports. Heavily table-tested.
- Spec: sections 4.2, 4.3, 5.5.

### Step 9: Statistics and usage store (`feat/stats`)
- Migrations `002_error_events` and `003_llm_usage`.
- Recording on manual Check with fingerprint and `source_key` rules, moving events on Save As, Not an error and the ignore set, grouped counts with 7 / 30 day windows (fake clock), usage records and totals, resets.
- Tags: `ErrorEventSaveFailed`, `StatsQueryFailed`, `StatsResetFailed`, `UsageSaveFailed`.
- Spec: sections 5.7, 5.8, 8.

### Step 10: Main window (`feat/ui-window`)
- miqt `QMainWindow`: toolbars, nested splitters, editor with word wrap, status bar, legend, geometry and splitter persistence.
- Application service wiring in `internal/app`; `main.go` starts it.
- Spec: sections 4.1, 4.2 (layout part), 9.

### Step 11: Preferences (`feat/ui-preferences`)
- Preferences dialog: LLM list (add, edit, delete, test connection) and the General tab (auto-check off by default, delay, font size).
- LLM selector in the toolbar with remembered selection and the empty state.
- Spec: sections 4.1, 4.5.

### Step 12: Check and underlines (`feat/ui-check`)
- Check / Cancel button, category colours and line styles, selected-issue highlight, tooltips, legend toggles, `F7` / `Shift+F7`, optional auto-check with debounce, status bar (state, counts, tokens).
- Spec: sections 4.1, 4.2, 5.1, 5.5.

### Step 13: Issues list and explanation panel (`feat/ui-explain`)
- Issues list with the shared selection, empty states, context menu; explanation panel with markdown streaming, states, header with counts, Not an error, Regenerate, link confirmation.
- Spec: sections 4.3, 4.4, 5.4, 5.9.

### Step 14: Statistics dialog (`feat/ui-statistics`)
- Statistics tab (table, filters, examples, reset) and Token usage tab.
- Spec: sections 4.6, 5.7, 5.8.

### Step 15: Saving (`feat/saving`)
- New, Open, Save, Save As with UTF-8 handling and unsaved-changes prompts; Save to library and the Library dialog; `source_key` switching.
- Tags: `FileReadFailed`, `FileWriteFailed`, `FileNotUTF8`.
- Spec: sections 5.7 (sources), 7.

### Step 16: Polish and README (`feat/polish`)
- README with Qt 6 build steps, manual smoke checklist, remaining spec items (privacy note for non-local providers, shortcuts), final pass over the spec's open points.
