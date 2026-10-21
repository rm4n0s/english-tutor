package gui

import (
	"regexp"
	"strconv"
	"strings"
)

// The explanation panel shows markdown written by an LLM. The text is untrusted
// (section 5.9 of the spec), so the parser is small and understands what the
// explanation prompt asks for: paragraphs, headings, bold, italic, code, lists,
// quotes and rules. Raw HTML is shown as text, and an image is shown as its alt
// text. Whether a link is followed is decided later, by view.DecideLink.

type blockKind int

const (
	blockParagraph blockKind = iota
	blockHeading
	blockCode
	blockRule
	blockQuote
	blockList
)

// mdBlock is one block of the document. Paragraphs and headings have Inlines;
// code has Code; a quote has Blocks; a list has Items, each a list of inlines.
type mdBlock struct {
	Kind    blockKind
	Level   int // of a heading, 1 to 6
	Inlines []mdInline
	Code    string
	Blocks  []mdBlock
	Items   [][]mdInline
	Ordered bool
}

// mdInline is a run of text with one look.
type mdInline struct {
	Text         string
	Bold, Italic bool
	Code         bool
	Link         string // the target when the run is a link
}

var (
	headingRe = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	ruleRe    = regexp.MustCompile(`^(\*{3,}|-{3,}|_{3,})$`)
	listRe    = regexp.MustCompile(`^(\s*)([-*+]|\d+[.)])\s+(.*)$`)
	orderedRe = regexp.MustCompile(`^\s*\d+[.)]\s`)
)

// parseMarkdown turns markdown into blocks.
func parseMarkdown(md string) []mdBlock {
	md = strings.ReplaceAll(strings.ReplaceAll(md, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(md, "\n")
	var out []mdBlock
	var para []string
	flush := func() {
		if len(para) > 0 {
			out = append(out, mdBlock{Kind: blockParagraph, Inlines: parseInline(strings.Join(para, " "))})
			para = nil
		}
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
			out = append(out, mdBlock{Kind: blockCode, Code: strings.Join(code, "\n")})
		case headingRe.MatchString(trimmed):
			flush()
			m := headingRe.FindStringSubmatch(trimmed)
			out = append(out, mdBlock{Kind: blockHeading, Level: len(m[1]), Inlines: parseInline(strings.TrimRight(m[2], "# "))})
		case ruleRe.MatchString(trimmed):
			flush()
			out = append(out, mdBlock{Kind: blockRule})
		case strings.HasPrefix(trimmed, ">"):
			flush()
			var quote []string
			for ; i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), ">"); i++ {
				quote = append(quote, strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(lines[i]), ">"), " "))
			}
			i--
			out = append(out, mdBlock{Kind: blockQuote, Blocks: parseMarkdown(strings.Join(quote, "\n"))})
		case listRe.MatchString(line):
			flush()
			ordered := orderedRe.MatchString(line)
			var items [][]mdInline
			var last string
			for ; i < len(lines); i++ {
				l := strings.TrimRight(lines[i], " \t")
				if m := listRe.FindStringSubmatch(l); m != nil && orderedRe.MatchString(l) == ordered {
					items = append(items, nil)
					last = m[3]
					items[len(items)-1] = parseInline(last)
					continue
				}
				// A more indented line continues the item above it.
				if strings.TrimSpace(l) != "" && len(items) > 0 && strings.HasPrefix(l, "  ") && !listRe.MatchString(l) {
					last += " " + strings.TrimSpace(l)
					items[len(items)-1] = parseInline(last)
					continue
				}
				break
			}
			i--
			out = append(out, mdBlock{Kind: blockList, Items: items, Ordered: ordered})
		default:
			para = append(para, trimmed)
		}
	}
	flush()
	return out
}

// parseInline reads bold, italic, code, links and images in a line of text.
func parseInline(s string) []mdInline {
	var out []mdInline
	var plain strings.Builder
	bold, italic := false, false
	emit := func(text string, in mdInline) {
		if text == "" {
			return
		}
		in.Text = text
		out = append(out, in)
	}
	flushPlain := func() {
		emit(plain.String(), mdInline{Bold: bold, Italic: italic})
		plain.Reset()
	}
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s) && strings.IndexByte("\\`*_{}[]()#+-.!>", s[i+1]) >= 0:
			plain.WriteByte(s[i+1])
			i += 2
		case c == '`':
			end := strings.IndexByte(s[i+1:], '`')
			if end < 0 {
				plain.WriteByte(c)
				i++
				continue
			}
			flushPlain()
			emit(s[i+1:i+1+end], mdInline{Code: true})
			i += end + 2
		case c == '!' && i+1 < len(s) && s[i+1] == '[':
			if text, _, n, ok := parseLink(s[i+1:]); ok {
				plain.WriteString(text) // an image is shown as its alt text
				i += 1 + n
				continue
			}
			plain.WriteByte(c)
			i++
		case c == '[':
			if text, target, n, ok := parseLink(s[i:]); ok {
				flushPlain()
				emit(text, mdInline{Bold: bold, Italic: italic, Link: target})
				i += n
				continue
			}
			plain.WriteByte(c)
			i++
		case (c == '*' || c == '_') && i+1 < len(s) && s[i+1] == c:
			flushPlain()
			bold = !bold
			i += 2
		case (c == '*' || c == '_') && emphasisAllowed(s, i, italic):
			flushPlain()
			italic = !italic
			i++
		default:
			plain.WriteByte(c)
			i++
		}
	}
	flushPlain()
	return out
}

// emphasisAllowed says whether the * or _ at i opens or closes italics. An
// underscore inside a word (snake_case) is text; so is a star around spaces.
func emphasisAllowed(s string, i int, open bool) bool {
	before := i == 0 || s[i-1] == ' '
	after := i+1 >= len(s) || s[i+1] == ' '
	if open { // closing
		return !before
	}
	if s[i] == '_' && !before {
		return false
	}
	return !after && strings.IndexByte(s[i+1:], s[i]) >= 0
}

// parseLink reads [text](target) at the start of s and returns the number of
// bytes it takes.
func parseLink(s string) (text, target string, n int, ok bool) {
	closeText := strings.IndexByte(s, ']')
	if closeText < 0 || closeText+1 >= len(s) || s[closeText+1] != '(' {
		return "", "", 0, false
	}
	closeTarget := strings.IndexByte(s[closeText+2:], ')')
	if closeTarget < 0 {
		return "", "", 0, false
	}
	target = strings.TrimSpace(s[closeText+2 : closeText+2+closeTarget])
	if i := strings.IndexByte(target, ' '); i >= 0 {
		target = target[:i] // a title after the address is dropped
	}
	return s[1:closeText], target, closeText + 3 + closeTarget, true
}

// plainText is the text of the blocks without markup, one block per line; tests
// and the screen reader's description read it.
func plainText(blocks []mdBlock) string {
	var lines []string
	runs := func(in []mdInline) string {
		var b strings.Builder
		for _, r := range in {
			b.WriteString(r.Text)
		}
		return b.String()
	}
	for _, b := range blocks {
		switch b.Kind {
		case blockParagraph, blockHeading:
			lines = append(lines, runs(b.Inlines))
		case blockCode:
			lines = append(lines, b.Code)
		case blockQuote:
			lines = append(lines, plainText(b.Blocks))
		case blockList:
			for i, item := range b.Items {
				mark := "• "
				if b.Ordered {
					mark = strconv.Itoa(i+1) + ". "
				}
				lines = append(lines, mark+runs(item))
			}
		}
	}
	return strings.Join(lines, "\n")
}
