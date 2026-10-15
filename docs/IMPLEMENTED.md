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
