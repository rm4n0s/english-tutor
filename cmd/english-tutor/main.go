// Command english-tutor is a desktop writing tutor that underlines English
// errors found by an LLM.
package main

import (
	"os"

	"github.com/rm4n0s/english-tutor/internal/app"
)

func main() {
	os.Exit(app.Run(os.Args))
}
