package analysis

import "unicode/utf8"

// Go strings are UTF-8 and Qt text positions are UTF-16 code units. The
// locator works in bytes; UTF16Map converts at the boundary to the UI.

const markEvery = 1024 // bytes between checkpoints

// UTF16Map converts offsets between a text's UTF-8 bytes and UTF-16 code
// units. It is built once per text and is safe for concurrent reads.
//
// An invalid UTF-8 byte counts as one code unit, like Qt's replacement
// character. A runes above U+FFFF counts as two (a surrogate pair).
type UTF16Map struct {
	text  string
	marks []mark // marks[i] describes the first rune boundary at or after i*markEvery
}

type mark struct{ byteOff, unitOff int }

// NewUTF16Map builds the map for text.
func NewUTF16Map(text string) *UTF16Map {
	m := &UTF16Map{text: text}
	units := 0
	for b := 0; b < len(text); {
		if b >= len(m.marks)*markEvery {
			m.marks = append(m.marks, mark{b, units})
			continue // re-check: a long rune may skip a whole interval
		}
		r, w := utf8.DecodeRuneInString(text[b:])
		units += unitsOf(r)
		b += w
	}
	return m
}

func unitsOf(r rune) int {
	if r >= 0x10000 {
		return 2
	}
	return 1
}

// Units returns the length of the whole text in UTF-16 code units.
func (m *UTF16Map) Units() int { return m.Offset(len(m.text)) }

// Offset returns the UTF-16 offset of the byte offset b. A b inside a rune
// gives the offset of that rune's start; b is clamped to the text.
func (m *UTF16Map) Offset(b int) int {
	b = max(0, min(b, len(m.text)))
	pos, units := m.checkpointForByte(b)
	for pos < len(m.text) {
		r, w := utf8.DecodeRuneInString(m.text[pos:])
		if pos+w > b {
			break // b is at pos or inside this rune
		}
		units += unitsOf(r)
		pos += w
	}
	return units
}

// ByteOffset is the inverse of Offset. A u inside a surrogate pair gives the
// byte offset of that rune's start; u is clamped to the text.
func (m *UTF16Map) ByteOffset(u int) int {
	u = max(0, u)
	pos, units := m.checkpointForUnit(u)
	for pos < len(m.text) {
		r, w := utf8.DecodeRuneInString(m.text[pos:])
		n := unitsOf(r)
		if units+n > u {
			break
		}
		units += n
		pos += w
	}
	return pos
}

func (m *UTF16Map) checkpointForByte(b int) (pos, units int) {
	// Last mark at or before b.
	lo, hi := 0, len(m.marks)
	for lo < hi {
		mid := (lo + hi) / 2
		if m.marks[mid].byteOff <= b {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo == 0 {
		return 0, 0
	}
	return m.marks[lo-1].byteOff, m.marks[lo-1].unitOff
}

func (m *UTF16Map) checkpointForUnit(u int) (pos, units int) {
	lo, hi := 0, len(m.marks)
	for lo < hi {
		mid := (lo + hi) / 2
		if m.marks[mid].unitOff <= u {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo == 0 {
		return 0, 0
	}
	return m.marks[lo-1].byteOff, m.marks[lo-1].unitOff
}
