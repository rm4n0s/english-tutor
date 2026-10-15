// Package specs holds a test that keeps docs/SPEC.md and the tests in step: every
// error tag in the spec's error table must be asserted by a route test.
package specs

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	tagPattern = regexp.MustCompile("`([A-Z][A-Za-z]+)`")
	routeCalls = []string{"HasRoute", "assertRoute", "assertTag"}
)

// specTags returns the tags named in the first column of the error table in
// section 10 of the spec.
func specTags(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "SPEC.md"))
	if err != nil {
		t.Fatal(err)
	}
	var tags []string
	inSection := false
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "## 10."):
			inSection = true
		case strings.HasPrefix(line, "## "):
			inSection = false
		case inSection && strings.HasPrefix(line, "| `"):
			cells := strings.Split(line, "|")
			for _, m := range tagPattern.FindAllStringSubmatch(cells[1], -1) {
				tags = append(tags, m[1])
			}
		}
	}
	return tags
}

// testSources returns the text of every _test.go file under internal/.
func testSources(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(filepath.Join("..", "..", "internal"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[path] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestEveryTagInTheSpecHasARouteTest(t *testing.T) {
	tags := specTags(t)
	if len(tags) < 20 {
		t.Fatalf("found only %d tags in the spec's error table; the table or this parser changed", len(tags))
	}
	files := testSources(t)
	for _, tag := range tags {
		found := false
		for path, src := range files {
			if strings.Contains(src, tag) && containsAny(src, routeCalls) && !strings.HasSuffix(path, "spec_test.go") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("tag %s is in the spec but no test file asserts its route", tag)
		}
	}
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
