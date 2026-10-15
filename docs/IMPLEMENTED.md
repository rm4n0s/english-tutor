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
