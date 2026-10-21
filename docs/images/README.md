# Images of the tutorial

The pictures in [TUTORIAL.md](../TUTORIAL.md) are screenshots of the real window and dialogs, drawn offscreen with a fake LLM (so they need no model and no display), and numbered callouts added on one of them.

To make them again after the look of the app changed, from the repository root:

```
DOC_SCREENSHOTS_DIR=$PWD/docs/images go test ./internal/app -run TestDocScreenshots
python3 docs/images/annotate.py
```

The first command (the test is in `internal/app/docshots_test.go`, and is skipped when the variable is not set) writes `01-write.png` to `09-llm-form.png`; the folder must be an absolute path, because tests run in the package folder. The second needs [Pillow](https://python-pillow.org) and draws the numbers of `overview.png` on `03-explanation.png`. If a window part moved, change the positions at the top of `annotate.py`.

The text and the mistakes of the pictures are in `docshots_test.go`.
