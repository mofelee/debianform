package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode"
)

type Block struct {
	Type  string
	Label string
	Attrs map[string]Expr
	File  string
	Line  int
}

type Expr interface{}

type StringLit string
type HeredocLit string
type Ref string
type Number string

type List []Expr
type Map map[string]Expr

type FuncCall struct {
	Name string
	Args []Expr
}

type parser struct {
	src  string
	file string
	pos  int
	line int
}

func ParseFile(file string) ([]Block, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	p := &parser{src: string(data), file: file, line: 1}
	return p.parseBlocks()
}

func (p *parser) parseBlocks() ([]Block, error) {
	var blocks []Block
	for {
		p.skipSpace()
		if p.eof() {
			return blocks, nil
		}
		line := p.line
		typ, err := p.parseIdent()
		if err != nil {
			return nil, err
		}
		p.skipInlineSpace()
		label := ""
		if p.peek() == '"' {
			expr, err := p.parseString()
			if err != nil {
				return nil, err
			}
			label = string(expr.(StringLit))
		}
		p.skipSpace()
		if err := p.expect('{'); err != nil {
			return nil, err
		}
		attrs, err := p.parseBody()
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, Block{Type: typ, Label: label, Attrs: attrs, File: p.file, Line: line})
	}
}

func (p *parser) parseBody() (map[string]Expr, error) {
	attrs := map[string]Expr{}
	for {
		p.skipSpace()
		if p.eof() {
			return nil, p.err("unexpected end of file in block")
		}
		if p.peek() == '}' {
			p.advance()
			return attrs, nil
		}
		key, err := p.parseIdent()
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		if err := p.expect('='); err != nil {
			return nil, err
		}
		p.skipSpace()
		value, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		attrs[key] = value
	}
}

func (p *parser) parseExpr() (Expr, error) {
	p.skipSpace()
	switch p.peek() {
	case '"':
		return p.parseString()
	case '[':
		return p.parseList()
	case '{':
		return p.parseMap()
	case '<':
		if strings.HasPrefix(p.src[p.pos:], "<<") {
			return p.parseHeredoc()
		}
	}

	token := p.parseBareToken()
	if token == "" {
		return nil, p.err("expected expression")
	}
	p.skipInlineSpace()
	if p.peek() == '(' {
		p.advance()
		var args []Expr
		for {
			p.skipSpace()
			if p.peek() == ')' {
				p.advance()
				break
			}
			arg, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			args = append(args, arg)
			p.skipSpace()
			if p.peek() == ',' {
				p.advance()
			}
		}
		return FuncCall{Name: token, Args: args}, nil
	}

	switch token {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	if isNumber(token) {
		return Number(token), nil
	}
	return Ref(token), nil
}

func (p *parser) parseString() (Expr, error) {
	if err := p.expect('"'); err != nil {
		return nil, err
	}
	var b strings.Builder
	for !p.eof() {
		ch := p.advance()
		if ch == '"' {
			return StringLit(b.String()), nil
		}
		if ch == '\\' && !p.eof() {
			next := p.advance()
			switch next {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case '"', '\\':
				b.WriteByte(next)
			default:
				b.WriteByte('\\')
				b.WriteByte(next)
			}
			continue
		}
		b.WriteByte(ch)
	}
	return nil, p.err("unterminated string")
}

func (p *parser) parseHeredoc() (Expr, error) {
	if err := p.expect('<'); err != nil {
		return nil, err
	}
	if err := p.expect('<'); err != nil {
		return nil, err
	}
	dedent := false
	if p.peek() == '-' {
		dedent = true
		p.advance()
	}
	start := p.pos
	for !p.eof() && p.peek() != '\n' {
		p.advance()
	}
	marker := strings.TrimSpace(p.src[start:p.pos])
	if marker == "" {
		return nil, p.err("heredoc marker is empty")
	}
	if p.peek() == '\n' {
		p.advance()
	}
	contentStart := p.pos
	for !p.eof() {
		lineStart := p.pos
		for !p.eof() && p.peek() != '\n' {
			p.advance()
		}
		line := p.src[lineStart:p.pos]
		if strings.TrimSpace(line) == marker {
			content := p.src[contentStart:lineStart]
			if p.peek() == '\n' {
				p.advance()
			}
			if dedent {
				content = stripIndent(content)
			}
			return HeredocLit(content), nil
		}
		if p.peek() == '\n' {
			p.advance()
		}
	}
	return nil, p.err("unterminated heredoc")
}

func (p *parser) parseList() (Expr, error) {
	if err := p.expect('['); err != nil {
		return nil, err
	}
	var out List
	for {
		p.skipSpace()
		if p.peek() == ']' {
			p.advance()
			return out, nil
		}
		value, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		out = append(out, value)
		p.skipSpace()
		if p.peek() == ',' {
			p.advance()
		}
	}
}

func (p *parser) parseMap() (Expr, error) {
	if err := p.expect('{'); err != nil {
		return nil, err
	}
	out := Map{}
	for {
		p.skipSpace()
		if p.peek() == '}' {
			p.advance()
			return out, nil
		}
		key, err := p.parseMapKey()
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		if err := p.expect('='); err != nil {
			return nil, err
		}
		p.skipSpace()
		value, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		out[key] = value
		p.skipSpace()
		if p.peek() == ',' {
			p.advance()
		}
	}
}

func (p *parser) parseMapKey() (string, error) {
	if p.peek() == '"' {
		expr, err := p.parseString()
		if err != nil {
			return "", err
		}
		return string(expr.(StringLit)), nil
	}
	start := p.pos
	for !p.eof() {
		ch := p.peek()
		if unicode.IsSpace(rune(ch)) || ch == '=' {
			break
		}
		p.advance()
	}
	key := strings.TrimSpace(p.src[start:p.pos])
	if key == "" {
		return "", p.err("expected map key")
	}
	return key, nil
}

func (p *parser) parseIdent() (string, error) {
	start := p.pos
	for !p.eof() {
		ch := p.peek()
		if ch == '_' || ch == '-' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' {
			p.advance()
			continue
		}
		break
	}
	if start == p.pos {
		return "", p.err("expected identifier")
	}
	return p.src[start:p.pos], nil
}

func (p *parser) parseBareToken() string {
	start := p.pos
	brackets := 0
	inQuote := false
	escaped := false
	for !p.eof() {
		ch := p.peek()
		if inQuote {
			p.advance()
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				inQuote = false
			}
			continue
		}
		switch ch {
		case '"':
			inQuote = true
		case '[':
			brackets++
		case ']':
			if brackets > 0 {
				brackets--
			} else {
				return p.src[start:p.pos]
			}
		case ',', '}', ')':
			if brackets == 0 {
				return p.src[start:p.pos]
			}
		case '(':
			if brackets == 0 {
				return p.src[start:p.pos]
			}
		default:
			if brackets == 0 && unicode.IsSpace(rune(ch)) {
				return p.src[start:p.pos]
			}
		}
		p.advance()
	}
	return p.src[start:p.pos]
}

func (p *parser) skipSpace() {
	for !p.eof() {
		ch := p.peek()
		if unicode.IsSpace(rune(ch)) {
			p.advance()
			continue
		}
		if ch == '#' {
			p.skipLine()
			continue
		}
		if ch == '/' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '/' {
			p.skipLine()
			continue
		}
		return
	}
}

func (p *parser) skipInlineSpace() {
	for !p.eof() {
		ch := p.peek()
		if ch == ' ' || ch == '\t' || ch == '\r' {
			p.advance()
			continue
		}
		return
	}
}

func (p *parser) skipLine() {
	for !p.eof() && p.peek() != '\n' {
		p.advance()
	}
}

func (p *parser) expect(ch byte) error {
	if p.eof() || p.peek() != ch {
		return p.err(fmt.Sprintf("expected %q", ch))
	}
	p.advance()
	return nil
}

func (p *parser) advance() byte {
	ch := p.src[p.pos]
	p.pos++
	if ch == '\n' {
		p.line++
	}
	return ch
}

func (p *parser) peek() byte {
	if p.eof() {
		return 0
	}
	return p.src[p.pos]
}

func (p *parser) eof() bool {
	return p.pos >= len(p.src)
}

func (p *parser) err(msg string) error {
	return fmt.Errorf("%s:%d: %s", p.file, p.line, msg)
}

func stripIndent(s string) string {
	lines := strings.Split(s, "\n")
	minIndent := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if minIndent == -1 || indent < minIndent {
			minIndent = indent
		}
	}
	if minIndent <= 0 {
		return s
	}
	for i, line := range lines {
		if len(line) >= minIndent {
			lines[i] = line[minIndent:]
		}
	}
	return strings.Join(lines, "\n")
}

func isNumber(token string) bool {
	if token == "" {
		return false
	}
	if _, err := strconv.ParseFloat(token, 64); err == nil {
		return true
	}
	return false
}
