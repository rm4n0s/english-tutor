package web

import (
	"strings"
	"testing"
)

func TestRenderMarkdown(t *testing.T) {
	tests := []struct {
		name string
		md   string
		want string // a fragment the output must contain
	}{
		{"paragraph", "Hello world.", "<p>Hello world.</p>"},
		{"two paragraphs", "One.\n\nTwo.", "<p>One.</p>\n<p>Two.</p>"},
		{"soft break joins lines", "one\ntwo", "<p>one two</p>"},
		{"bold", "a **bold** word", "a <strong>bold</strong> word"},
		{"italic star", "an *italic* word", "an <em>italic</em> word"},
		{"italic underscore", "an _italic_ word", "an <em>italic</em> word"},
		{"snake case is not italic", "use snake_case_name here", "snake_case_name"},
		{"inline code", "say `goes` here", "say <code>goes</code> here"},
		{"code is escaped", "`<b>x</b>`", "<code>&lt;b&gt;x&lt;/b&gt;</code>"},
		{"heading", "# What is wrong", "<h3>What is wrong</h3>"},
		{"deep heading is capped", "###### Tiny", "<h6>Tiny</h6>"},
		{"list", "- one\n- two", "<ul><li>one</li><li>two</li></ul>"},
		{"ordered list", "1. one\n2. two", "<ol><li>one</li><li>two</li></ol>"},
		{"list item continues", "- one\n  more\n- two", "<li>one more</li><li>two</li>"},
		{"quote", "> be kind", "<blockquote><p>be kind</p>\n</blockquote>"},
		{"rule", "---", "<hr>"},
		{"fenced code", "```\nHe go\n```", "<pre><code>He go</code></pre>"},
		{"link", "[docs](https://example.com/a?b=1&c=2)", `<a href="https://example.com/a?b=1&amp;c=2" target="_blank" rel="noopener noreferrer nofollow" class="ext">docs</a>`},
		{"empty link text shows the address", "[](https://example.com)", ">https://example.com</a>"},
		{"escaped marker", `2 \* 3`, "2 * 3"},
		{"unmatched bold stays", "a ** b", "a ** b"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(renderMarkdown(tt.md))
			if !strings.Contains(got, tt.want) {
				t.Errorf("renderMarkdown(%q) = %q; want it to contain %q", tt.md, got, tt.want)
			}
		})
	}
}

// allowedTags are the only tags renderMarkdown may write.
var allowedTags = []string{"p>", "h3>", "h4>", "h5>", "h6>", "strong>", "em>", "code>", "pre>", "ul>", "ol>", "li>",
	"blockquote>", "hr>", "a>", `a href="http://`, `a href="https://`}

// unexpectedTag returns the first tag in html that the renderer must not write.
func unexpectedTag(html string) (string, bool) {
	for _, frag := range strings.Split(html, "<")[1:] {
		name := strings.ToLower(strings.TrimPrefix(frag, "/"))
		ok := false
		for _, allowed := range allowedTags {
			if strings.HasPrefix(name, allowed) {
				ok = true
			}
		}
		if !ok {
			return frag, true
		}
	}
	return "", false
}

// An explanation comes from an LLM that has read the user's text, so nothing in
// it may become active HTML (section 5.9).
func TestRenderMarkdownNeverProducesActiveContent(t *testing.T) {
	attacks := []string{
		"<script>alert(1)</script>",
		"<img src=x onerror=alert(1)>",
		"[click](javascript:alert(1))",
		"[click](JaVaScRiPt:alert(1))",
		"[click](data:text/html;base64,PHNjcmlwdD4=)",
		"[click](file:///etc/passwd)",
		"[click](//evil.example/x)",
		"[click](/relative)",
		"[click](preferences:)",
		"![x](http://evil.example/track.png)",
		"![x](javascript:alert(1))",
		"**<b onmouseover=x>**",
		"[<img src=x onerror=alert(1)>](https://example.com)",
		"`</code><script>alert(1)</script>`",
		"> <iframe src=//evil.example>",
		"- <style>*{}</style>",
		"# <svg onload=alert(1)>",
		"```\n</pre><script>alert(1)</script>\n```",
	}
	for _, a := range attacks {
		got := string(renderMarkdown(a))
		if frag, bad := unexpectedTag(got); bad {
			t.Errorf("renderMarkdown(%q) = %q has an unexpected tag at %q", a, got, frag)
		}
		if strings.Contains(strings.ToLower(got), "href=\"javascript") {
			t.Errorf("renderMarkdown(%q) = %q has an active link", a, got)
		}
	}
}

func TestRenderMarkdownLinkAttributeCannotBreakOut(t *testing.T) {
	got := string(renderMarkdown(`[a](https://x.example/"onmouseover="alert(1))`))
	if strings.Contains(got, `"onmouseover=`) {
		t.Errorf("the quote in the address broke out of the attribute: %q", got)
	}
}

func FuzzRenderMarkdown(f *testing.F) {
	for _, s := range []string{"", "**a**", "[a](https://b.example)", "`x", "*", "![", "[[]]((", "1. a\n  b\n2. c", "> > >", "```", "\\", "_a_b_"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got := string(renderMarkdown(s))
		if frag, bad := unexpectedTag(got); bad {
			t.Fatalf("renderMarkdown(%q) = %q has an unexpected tag at %q", s, got, frag)
		}
	})
}
