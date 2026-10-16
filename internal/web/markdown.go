package web

import (
	"html"
	"html/template"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rm4n0s/english-tutor/internal/ui"
)

// renderMarkdown turns an explanation written by an LLM into HTML.
//
// The text is untrusted (section 5.9 of the spec), so the renderer is small and
// escapes first: every character that is not markdown syntax is HTML-escaped,
// raw HTML in the text is shown as text, an image is shown as its alt text and
// a link becomes an anchor only when ui.DecideLink would offer it (http or https
// with a host). It understands what the explanation prompt asks for:
// paragraphs, headings, bold, italic, code, lists, quotes and rules.
func renderMarkdown(md string) template.HTML {
	md = strings.ReplaceAll(strings.ReplaceAll(md, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(md, "\n")
	var out strings.Builder
	var para []string

	flush := func() {
		if len(para) == 0 {
			return
		}
		out.WriteString("<p>" + renderInline(strings.Join(para, " ")) + "</p>\n")
		para = nil
	}

	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], " \t")
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			flush()
		case strings.HasPrefix(trimmed, "```"):
			flush()
			var code []string
			for i++; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```"); i++ {
				code = append(code, lines[i])
			}
			out.WriteString("<pre><code>" + html.EscapeString(strings.Join(code, "\n")) + "</code></pre>\n")
		case headingRe.MatchString(trimmed):
			flush()
			m := headingRe.FindStringSubmatch(trimmed)
			level := min(len(m[1])+2, 6) // the panel's own title is the h2
			out.WriteString(tag(level, renderInline(strings.TrimRight(m[2], "# "))))
		case ruleRe.MatchString(trimmed):
			flush()
			out.WriteString("<hr>\n")
		case strings.HasPrefix(trimmed, ">"):
			flush()
			var quote []string
			for ; i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), ">"); i++ {
				quote = append(quote, strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(lines[i]), ">"), " "))
			}
			i--
			out.WriteString("<blockquote>" + string(renderMarkdown(strings.Join(quote, "\n"))) + "</blockquote>\n")
		case listRe.MatchString(line):
			flush()
			ordered := orderedRe.MatchString(line)
			var items []string
			for ; i < len(lines); i++ {
				l := strings.TrimRight(lines[i], " \t")
				if m := listRe.FindStringSubmatch(l); m != nil && orderedRe.MatchString(l) == ordered {
					items = append(items, m[2])
					continue
				}
				// A more indented line continues the item above it.
				if strings.TrimSpace(l) != "" && len(items) > 0 && strings.HasPrefix(l, "  ") && !listRe.MatchString(l) {
					items[len(items)-1] += " " + strings.TrimSpace(l)
					continue
				}
				break
			}
			i--
			name := "ul"
			if ordered {
				name = "ol"
			}
			out.WriteString("<" + name + ">")
			for _, it := range items {
				out.WriteString("<li>" + renderInline(it) + "</li>")
			}
			out.WriteString("</" + name + ">\n")
		default:
			para = append(para, trimmed)
		}
	}
	flush()
	return template.HTML(out.String()) //nolint:gosec // every piece was escaped above
}

var (
	headingRe = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	ruleRe    = regexp.MustCompile(`^(?:-{3,}|\*{3,}|_{3,})$`)
	listRe    = regexp.MustCompile(`^\s{0,3}([-*+]|\d{1,9}[.)])\s+(.*)$`)
	orderedRe = regexp.MustCompile(`^\s{0,3}\d{1,9}[.)]\s`)
)

func tag(level int, inner string) string {
	n := string(rune('0' + level))
	return "<h" + n + ">" + inner + "</h" + n + ">\n"
}

// renderInline renders bold, italic, code, links and escapes. Unmatched markers
// stay in the text.
func renderInline(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		rest := s[i:]
		switch {
		case c == '\\' && i+1 < len(s) && strings.IndexByte("\\`*_{}[]()#+-.!<>|~", s[i+1]) >= 0:
			b.WriteString(html.EscapeString(s[i+1 : i+2]))
			i += 2
		case c == '`':
			n := 1
			for n < len(rest) && rest[n] == '`' {
				n++
			}
			fence := strings.Repeat("`", n)
			if end := strings.Index(rest[n:], fence); end >= 0 {
				b.WriteString("<code>" + html.EscapeString(strings.TrimSpace(rest[n:n+end])) + "</code>")
				i += n + end + n
				continue
			}
			b.WriteString(html.EscapeString(fence))
			i += n
		case strings.HasPrefix(rest, "**") || strings.HasPrefix(rest, "__"):
			if end := strings.Index(rest[2:], rest[:2]); end > 0 {
				b.WriteString("<strong>" + renderInline(rest[2:2+end]) + "</strong>")
				i += 2 + end + 2
				continue
			}
			b.WriteString(html.EscapeString(rest[:2]))
			i += 2
		case (c == '*' || c == '_') && emphasisOpens(s, i):
			if end := emphasisEnd(rest, c); end > 0 {
				b.WriteString("<em>" + renderInline(rest[1:end]) + "</em>")
				i += end + 1
				continue
			}
			b.WriteString(html.EscapeString(rest[:1]))
			i++
		case c == '!' && strings.HasPrefix(rest, "!["):
			if text, _, n, ok := parseLink(rest[1:]); ok {
				b.WriteString(html.EscapeString(text)) // images are never loaded
				i += 1 + n
				continue
			}
			b.WriteString("!")
			i++
		case c == '[':
			if text, target, n, ok := parseLink(rest); ok {
				if ui.DecideLink(target, false) == ui.LinkConfirm {
					shown := renderInline(text)
					if strings.TrimSpace(text) == "" {
						shown = html.EscapeString(target)
					}
					b.WriteString(`<a href="` + html.EscapeString(target) +
						`" target="_blank" rel="noopener noreferrer nofollow" class="ext">` + shown + "</a>")
				} else {
					b.WriteString(renderInline(text))
				}
				i += n
				continue
			}
			b.WriteString("[")
			i++
		default:
			r, size := utf8.DecodeRuneInString(rest)
			if r == utf8.RuneError && size == 1 {
				b.WriteString("�")
			} else {
				b.WriteString(html.EscapeString(rest[:size]))
			}
			i += size
		}
	}
	return b.String()
}

// emphasisOpens reports whether the * or _ at s[i] can open emphasis: the next
// character is not a space, and an underscore inside a word (snake_case) does
// not count.
func emphasisOpens(s string, i int) bool {
	if i+1 >= len(s) || s[i+1] == ' ' || s[i+1] == s[i] {
		return false
	}
	if s[i] == '_' && i > 0 {
		prev, _ := utf8.DecodeLastRuneInString(s[:i])
		if unicode.IsLetter(prev) || unicode.IsDigit(prev) {
			return false
		}
	}
	return true
}

// emphasisEnd returns the index in rest (which starts with the marker) of the
// marker that closes it, or -1.
func emphasisEnd(rest string, marker byte) int {
	for j := 1; j < len(rest); j++ {
		if rest[j] != marker || rest[j-1] == ' ' {
			continue
		}
		if marker == '_' && j+1 < len(rest) {
			next, _ := utf8.DecodeRuneInString(rest[j+1:])
			if unicode.IsLetter(next) || unicode.IsDigit(next) {
				continue
			}
		}
		return j
	}
	return -1
}

// parseLink reads "[text](target)" at the start of s. n is its length in bytes.
func parseLink(s string) (text, target string, n int, ok bool) {
	depth := 0
	end := -1
	for j := 0; j < len(s) && end < 0; j++ {
		switch s[j] {
		case '\\':
			j++
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				end = j
			}
		}
	}
	if end < 0 || end+1 >= len(s) || s[end+1] != '(' {
		return "", "", 0, false
	}
	depth = 0
	for j := end + 1; j < len(s); j++ {
		switch s[j] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				target = strings.TrimSpace(s[end+2 : j])
				// A title after the address ("url "title"") is not part of it.
				if sp := strings.IndexAny(target, " \t"); sp >= 0 {
					target = target[:sp]
				}
				return s[1:end], target, j + 1, true
			}
		}
	}
	return "", "", 0, false
}
