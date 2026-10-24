package app

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

// Ranges use UTF-16 code units, matching browser selection/string offsets.
type RepairRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}
type RepairInfo struct {
	Added   int           `json:"added"`
	Removed int           `json:"removed"`
	Ranges  []RepairRange `json:"ranges"`
}

var errUnrepairable = errors.New("ambiguous or unsupported JSON repair")

// Repair punctuation without decoding values. Never invent a missing value,
// alter numeric spelling, merge duplicate keys, or discard unknown text.
// Bounded nesting and edit count keep malformed inputs inexpensive.
type repairParser struct {
	s       string
	i       int
	out     []byte
	marks   []bool
	removed int
	edits   int
	stack   []byte
}

func repairJSON(s string) (string, []bool, int, error) {
	p := &repairParser{s: s}
	if err := p.value(); err != nil {
		return "", nil, 0, err
	}
	p.space()
	// Redundant closing punctuation after a complete root value.
	for p.i < len(s) && strings.ContainsRune(",}]", rune(s[p.i])) {
		if err := p.remove(); err != nil {
			return "", nil, 0, err
		}
		p.space()
	}
	if p.i != len(s) || !json.Valid(p.out) {
		return "", nil, 0, errUnrepairable
	}
	return string(p.out), p.marks, p.removed, nil
}

func (p *repairParser) space() {
	for p.i < len(p.s) && strings.ContainsRune(" \t\r\n", rune(p.s[p.i])) {
		p.i++
	}
}
func (p *repairParser) emit(s string, added bool) {
	p.out = append(p.out, s...)
	for range len(s) {
		p.marks = append(p.marks, added)
	}
}
func (p *repairParser) insert(s string) error {
	p.edits += len(s)
	if p.edits > 256 {
		return errUnrepairable
	}
	p.emit(s, true)
	return nil
}
func (p *repairParser) remove() error {
	p.edits++
	if p.edits > 256 {
		return errUnrepairable
	}
	p.i++
	p.removed++
	return nil
}
func (p *repairParser) take() { p.emit(p.s[p.i:p.i+1], false); p.i++ }

func (p *repairParser) value() error {
	p.space()
	if p.i == len(p.s) {
		return errUnrepairable
	}
	switch p.s[p.i] {
	case '{':
		return p.container('}')
	case '[':
		return p.container(']')
	case '"', '\'':
		return p.stringValue(false)
	}
	start := p.i
	for p.i < len(p.s) && !strings.ContainsRune(" \t\r\n,]}:{[\"'", rune(p.s[p.i])) {
		p.i++
	}
	token := p.s[start:p.i]
	if token == "" || !json.Valid([]byte(token)) {
		return errUnrepairable
	}
	p.emit(token, false)
	return nil
}

func (p *repairParser) close(close byte) (bool, error) {
	p.space()
	if p.i == len(p.s) {
		return true, p.insert(string(close))
	}
	c := p.s[p.i]
	if c == close {
		p.take()
		return true, nil
	}
	if c != '}' && c != ']' {
		return false, nil
	}
	// If this closes an ancestor, close the current container first.
	for _, ancestor := range p.stack[:len(p.stack)-1] {
		if c == ancestor {
			return true, p.insert(string(close))
		}
	}
	// A mismatched closer has no owner; replace it with the expected closer.
	if err := p.remove(); err != nil {
		return true, err
	}
	return true, p.insert(string(close))
}

func (p *repairParser) container(close byte) error {
	if len(p.stack) >= 128 {
		return errUnrepairable
	}
	p.stack = append(p.stack, close)
	defer func() { p.stack = p.stack[:len(p.stack)-1] }()
	p.take()
	p.space()
	// Leading commas cannot represent a value in JSON.
	for p.i < len(p.s) && p.s[p.i] == ',' {
		if err := p.remove(); err != nil {
			return err
		}
		p.space()
	}
	if done, err := p.close(close); done {
		return err
	}
	for {
		if close == '}' {
			if err := p.key(); err != nil {
				return err
			}
			p.space()
			if p.i < len(p.s) && p.s[p.i] == ':' {
				p.take()
			} else if err := p.insert(":"); err != nil {
				return err
			}
			p.space()
			for p.i < len(p.s) && p.s[p.i] == ':' {
				if err := p.remove(); err != nil {
					return err
				}
				p.space()
			}
		}
		if err := p.value(); err != nil {
			return err
		}
		p.space()
		if done, err := p.close(close); done {
			return err
		}
		if p.s[p.i] == ',' {
			comma := p.i
			p.i++
			p.space()
			for p.i < len(p.s) && p.s[p.i] == ',' {
				if err := p.remove(); err != nil {
					return err
				}
				p.space()
			}
			if p.i == len(p.s) || p.s[p.i] == '}' || p.s[p.i] == ']' {
				p.removed++
				p.edits++
				if p.edits > 256 {
					return errUnrepairable
				}
				_, err := p.close(close)
				return err
			}
			p.emit(p.s[comma:comma+1], false)
		} else if err := p.insert(","); err != nil {
			return err
		}
	}
}

func (p *repairParser) key() error {
	p.space()
	if p.i == len(p.s) {
		return errUnrepairable
	}
	if p.s[p.i] == '"' || p.s[p.i] == '\'' {
		return p.stringValue(true)
	}
	start := p.i
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == ':' || c == '"' || strings.ContainsRune(" \t\r\n", rune(c)) {
			break
		}
		if strings.ContainsRune("{}[],\\'", rune(c)) || c < 0x20 {
			return errUnrepairable
		}
		p.i++
	}
	if p.i == start {
		return errUnrepairable
	}
	if err := p.insert("\""); err != nil {
		return err
	}
	p.emit(p.s[start:p.i], false)
	if p.i < len(p.s) && p.s[p.i] == '"' {
		p.take()
	} else if err := p.insert("\""); err != nil {
		return err
	}
	return nil
}

func (p *repairParser) stringValue(key bool) error {
	quote := p.s[p.i]
	if quote == '"' {
		p.take()
	} else {
		if err := p.remove(); err != nil {
			return err
		}
		if err := p.insert("\""); err != nil {
			return err
		}
	}
	start := p.i
	// First find a real terminator, respecting escape sequences. Do not treat
	// punctuation in a properly closed string as structure.
	end := -1
	for j := start; j < len(p.s); j++ {
		if p.s[j] == '\\' {
			j++
			continue
		}
		if p.s[j] == quote {
			end = j
			break
		}
		if p.s[j] < 0x20 {
			break
		}
	}
	if end < 0 {
		end = start
		if key {
			for end < len(p.s) && p.s[end] != ':' && p.s[end] >= 0x20 && p.s[end] != '"' {
				end++
			}
			if end == len(p.s) || p.s[end] != ':' {
				return errUnrepairable
			}
		} else {
			end = len(p.s)
			for end > start && strings.ContainsRune(" \t\r\n}]", rune(p.s[end-1])) {
				end--
			}
		}
		// An escaped final slash or internal quote makes the boundary ambiguous.
		fragment := p.s[start:end]
		if strings.ContainsAny(fragment, "\r\n\"") {
			return errUnrepairable
		}
		if !json.Valid([]byte("\"" + fragment + "\"")) {
			return errUnrepairable
		}
		p.emit(fragment, false)
		p.i = end
		return p.insert("\"")
	}
	for p.i < end {
		c := p.s[p.i]
		if c < 0x20 {
			return errUnrepairable
		}
		if c == '\\' {
			if p.i+1 >= end {
				return errUnrepairable
			}
			if quote == '\'' && p.s[p.i+1] == '\'' {
				if err := p.remove(); err != nil {
					return err
				}
				p.take()
			} else {
				p.take()
				p.take()
			}
		} else {
			if quote == '\'' && c == '"' {
				if err := p.insert("\\"); err != nil {
					return err
				}
			}
			p.take()
		}
	}
	if quote == '"' {
		p.take()
		return nil
	}
	if err := p.remove(); err != nil {
		return err
	}
	return p.insert("\"")
}

// Indent/Compact only change whitespace outside strings; align preserved bytes
// to transfer exact inserted-character provenance into the displayed output.
func repairInfo(compact, output string, marks []bool, removed int) *RepairInfo {
	info := &RepairInfo{Removed: removed, Ranges: make([]RepairRange, 0)}
	source, units := 0, 0
	quoted, escaped := false, false
	for i := 0; i < len(output); {
		r, size := utf8.DecodeRuneInString(output[i:])
		width := 1
		if r > 0xffff {
			width = 2
		}
		structuralSpace := !quoted && strings.ContainsRune(" \t\r\n", r)
		if !structuralSpace {
			if source < len(marks) && marks[source] {
				info.Added++
				n := len(info.Ranges)
				if n > 0 && info.Ranges[n-1].End == units {
					info.Ranges[n-1].End += width
				} else {
					info.Ranges = append(info.Ranges, RepairRange{units, units + width})
				}
			}
			source += size
		}
		if escaped {
			escaped = false
		} else if quoted && r == '\\' {
			escaped = true
		} else if r == '"' {
			quoted = !quoted
		}
		units += width
		i += size
	}
	return info
}
