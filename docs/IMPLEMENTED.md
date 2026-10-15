# Implemented steps

Finished steps of [IMPLEMENTATION.md](IMPLEMENTATION.md), newest last. Each entry is added on the step's branch before it is published to `main`.

## Step 1: Spec and plan
- Branch: feat/docs
- Date: 2026-10-15
- Spec: all sections (written, not implemented)
- Done: added `docs/SPEC.md`, `docs/IMPLEMENTATION.md` and `docs/IMPLEMENTED.md`.
- Checks: not applicable. There is no Go code yet, so gofmt, go vet, golangci-lint and go test have nothing to check and were not run.
- Notes: `CLAUDE.md` is not part of this step and stays untracked. Its "Project notes" section describes another project and needs the user's decision.

## Step 2: Project skeleton
- Branch: feat/skeleton
- Date: 2026-10-15
- Spec: sections 8 (location, logs), 9, 10
- Done: `go.mod` (module `github.com/rm4n0s/english-tutor`, one dependency: `github.com/rm4n0s/errors`), `.gitignore`, a thin `cmd/english-tutor/main.go`, `internal/config` (`HomeDir`, `NewPaths`, `Paths.Ensure`; tags `HomeDirNotFound`, `DataDirCreateFailed`) and `internal/logging` (`RotatingFile` with 5 MB size rotation and 3 old files, `New` with stderr fallback, `LogError` logging `ToJson()` output; tag `LogFileOpenFailed`). Route tests cover every tag.
- Checks: gofmt -l (clean), go vet (clean), golangci-lint run (0 issues), go test -race (pass). The binary was not run, because it would create `~/.english-tutor` outside the project.
- Notes: spec change: added the tag `LogRotateFailed` (rotation failure, section 10) so that rotation and open failures each have their own tag. `Paths.Ensure` also tightens the mode of an existing data directory to 0700; a chmod failure uses `DataDirCreateFailed` and is not covered by a test. `logging.New` returns no error: on failure it logs the `LogFileOpenFailed` record to stderr and uses stderr as the log.

## Step 3: Store
- Branch: feat/store
- Date: 2026-10-15
- Spec: sections 3, 8, 10
- Done: `internal/store` with `Store` (`Open` for the 0600 database file, `OpenMemory` for tests, `Close`); numbered migrations with a `schema_migrations` table and migration `001_init` (`llm_configs`, `documents`, `settings`, using frozen table structs); CRUD for LLM configs (unique name, per-provider validation), documents (newest first) and settings with typed getters and defaults (`auto_check` off, delay 3 s, font size 13). Tags `DatabaseOpenFailed`, `MigrationFailed`, `LLMConfigNameTaken`, `LLMConfigInvalid`, `LLMConfigNotFound`, `DocumentNotFound`, `DatabaseQueryFailed`, each with a route test; API keys never appear in error metadata (tested).
- Checks: gofmt -l (clean), go vet (clean), golangci-lint run (0 issues), go test -race (pass).
- Notes: the user approved `gorm.io/driver/sqlite` (cgo, `mattn/go-sqlite3`) for this step. Spec changes: added the tag `DatabaseQueryFailed` (any other query failure) and the setting defaults to sections 8 and 10. Provider names are plain string constants in `store` for now; step 4 introduces the `llm.Provider` enum and `store` should then use it (`llm` must not import `store`). A damaged setting value falls back to its default instead of failing. Validation: a base URL is required for `ollama` and `openai-compatible`, an API key for `anthropic` only, temperature 0 to 2.

## Step 4: LLM factory
- Branch: feat/llm
- Date: 2026-10-15
- Spec: sections 3, 4.5, 5.8, 10
- Done: `internal/llm` with the `Provider` enum and `ParseProvider`; `ModelConfig` and its `Validate` (the single place where provider, model, base URL, key and temperature are validated); `New(ModelConfig)` building the langchaingo model for ollama, openai-compatible and anthropic with all credentials and URLs passed explicitly; `Usage` with `UsageFromInfo`, `EstimateUsage`, `Add`, `Total`; `Generate` (system + user message, temperature, optional streaming callback, usage from the provider or estimated); `internal/llm/llmtest` with a `Fake` model (canned replies in order, errors, delays that honour the context, streamed chunks, recorded calls). `internal/store` now validates through `llm.ModelConfig` and its own provider constants were removed. Tags `UnknownProvider`, `LLMClientCreateFailed` (new route tests); `LLMConfigInvalid` is tested in `llm` too.
- Checks: gofmt -l (clean), go vet (clean), golangci-lint run (0 issues), go test -race (pass).
- Notes: langchaingo v0.1.15 added (approved dependency). Usage keys verified against its source and by `TestUsageFromRealClients`, which runs the real clients against local httptest servers: `PromptTokens`/`CompletionTokens` (openai-compatible, ollama), `InputTokens`/`OutputTokens` (anthropic). Streaming usage was not verified against real servers; zeros fall back to the estimate. Spec changes: usage key names; base URL must be http(s) because langchaingo's `ollama.WithServerURL` calls `log.Fatal` on an unparsable URL; the `LLMConfigInvalid` row. An empty answer from the model is returned as empty text with estimated usage (the analysis layer rejects it, step 5/7), not as a separate error. The temperature is a per-call option, so it is on `Request`, not on the model. Neither the real providers nor the `LLMClientCreateFailed` path from a real constructor were exercised: with validated input the constructors cannot fail, so that tag is tested through an injected builder.

## Step 5: Taxonomy and response parsing
- Branch: feat/analysis-parse
- Date: 2026-10-15
- Spec: sections 5.2, 5.6, 5.9
- Done: `internal/analysis` with the fixed taxonomy (23 types in three categories, `Types`, `Lookup`, `Resolve` mapping unknown ids to the `_other` type of the best-suggested category, defaulting to `grammar_other`); `BuildCheckPrompt` (system prompt with contract and type list; user message with the text between delimiter lines carrying a random per-request token that is never present in the text) and `WithRetryReminder`; `ParseResponse` (code fences and surrounding prose tolerated, a whole-reply bare array accepted, `issues` required, entries without text dropped and counted, length caps); `CheckText` (one request, one retry with the reminder on an invalid reply, usage summed over both requests, request errors returned untouched and never retried). Tag `LLMResponseInvalid` with route tests. A fuzz test for `ParseResponse` ran for 20 s (3.2 million inputs) without failures.
- Checks: gofmt -l (clean), go vet (clean), golangci-lint run (0 issues), go test -race (pass).
- Notes: spec change in 5.2 (tolerant parsing rules and caps). Decisions: the model's reply is never put in error metadata; `Chat` is `llm.Generate` bound to a model, so step 7 only has to wrap it; mapping `LLMCallFailed`, `LLMCallTimeout` and cancellation is step 7's job. `Resolve` uses simple keyword hints, so a made-up id can land in a surprising `_other` bucket; this only affects unknown ids. The prompt wording has not been tried against a real model (no provider is called in tests), so it may need tuning in the manual smoke check (step 16).

## Step 6: Locating and chunking
- Branch: feat/analysis-locate
- Date: 2026-10-15
- Spec: sections 5.1 (chunking), 5.3
- Done: in `internal/analysis`: `Locate` (unique match; several matches ranked by word boundaries, then context score, then first unclaimed; case-, whitespace-, quote- and dash-insensitive fallback mapped back to the original text; overlap removal keeping the earlier span; `Unplaced` and `Overlapping` counters) with `LocateResult.Append` to merge chunk results; `UTF16Map` (`Offset`, `ByteOffset`, `Units`, checkpoints every 1 KiB so long texts stay cheap); `SplitChunks` (paragraph, then sentence, then word cuts; contiguous chunks covering the whole text, never whitespace-only). Fuzz tests for `Locate`, `UTF16Map` (against `unicode/utf16`) and `SplitChunks`, each run for 20 to 40 s without failures after the fix below.
- Checks: gofmt -l (clean), go vet (clean), golangci-lint run (0 issues), go test -race (pass).
- Notes: fuzzing found a real bug: an invalid UTF-8 snippet (`\xbf`) matched inside a multi-byte character; matches must now start and end on rune boundaries, and the input is a regression case in `TestLocate` (the generated `testdata` file was removed). Spec changes in 5.1 and 5.3 (word-boundary preference, folding of quotes and dashes, chunk rules, `UTF16Map`). Known limits: sentence splitting is a simple rule (`. ! ?` followed by whitespace), so abbreviations like "Mr. Smith" can split a sentence; this only matters inside a paragraph over 2000 words. Locate runs per chunk, so an error that spans a chunk boundary is not found.

## Step 7: Analysis service
- Branch: feat/analysis-service
- Date: 2026-10-15
- Spec: sections 5.1, 5.4, 6, 10
- Done: in `internal/analysis`: `Model` (name, langchaingo model, temperature); `Analyzer` (`Check` splits the text into chunks, runs `CheckText` per chunk with a per-request timeout, locates and merges the issues, sums the usage; `Cancel`; one check at a time, a new check cancels the previous one; empty text makes no request); `Explainer` (`Explain` streams the markdown through a callback, cache by caller key with `Regenerate` and `Reset`, one explanation at a time, a cached read also stops a streaming one); `BuildExplainPrompt` and the explanation context rule; `UsageRecorder` interface (implemented by the store in step 9) and `UsageRecord`; shared `classifyError` (cancelled first, then module errors untouched, then timeout, then failure). Tags `LLMCallFailed`, `LLMCallTimeout` with route tests; `LLMResponseInvalid` also for an empty explanation. Cancelled checks and explanations return `Cancelled` and no error, record no error and log nothing above info (tested).
- Checks: gofmt -l (clean), go vet (clean), golangci-lint run (0 issues), go test -race (pass, also 10 repeats of the analysis package for flakiness).
- Notes: spec changes in 5.4 and section 6. Usage is recorded once per chunk (the sum of its two attempts if it was retried) and once per explanation; a recorder failure is logged as a warning and never fails the request. The services log summaries with token counts and timings only; a test checks that the user's text does not appear in the log. The failure message of `LLMCallFailed` contains the underlying error text from the provider client; provider messages normally mask keys, but this has not been checked against real providers. Unplaced count also includes entries the model returned without text.
