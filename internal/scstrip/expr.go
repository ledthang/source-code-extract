package scstrip

import "fmt"

// evalExpr evaluates a C# preprocessor boolean expression.
// Grammar (lowest to highest precedence): || , && , ==/!= , ! , primary.
// Primaries are symbols (true when defined), the literals true/false, and ( ).
func evalExpr(expr string, symbols map[string]bool) (bool, error) {
	toks, err := tokenize(expr)
	if err != nil {
		return false, err
	}
	p := &parser{toks: toks, symbols: symbols}
	v, err := p.parseOr()
	if err != nil {
		return false, err
	}
	if p.pos != len(p.toks) {
		return false, fmt.Errorf("unexpected token %q in #if expression", p.toks[p.pos])
	}
	return v, nil
}

type parser struct {
	toks    []string
	pos     int
	symbols map[string]bool
}

func (p *parser) peek() string {
	if p.pos < len(p.toks) {
		return p.toks[p.pos]
	}
	return ""
}

func (p *parser) parseOr() (bool, error) {
	v, err := p.parseAnd()
	if err != nil {
		return false, err
	}
	for p.peek() == "||" {
		p.pos++
		rhs, err := p.parseAnd()
		if err != nil {
			return false, err
		}
		v = v || rhs
	}
	return v, nil
}

func (p *parser) parseAnd() (bool, error) {
	v, err := p.parseEquality()
	if err != nil {
		return false, err
	}
	for p.peek() == "&&" {
		p.pos++
		rhs, err := p.parseEquality()
		if err != nil {
			return false, err
		}
		v = v && rhs
	}
	return v, nil
}

func (p *parser) parseEquality() (bool, error) {
	v, err := p.parseUnary()
	if err != nil {
		return false, err
	}
	for op := p.peek(); op == "==" || op == "!="; op = p.peek() {
		p.pos++
		rhs, err := p.parseUnary()
		if err != nil {
			return false, err
		}
		if op == "==" {
			v = v == rhs
		} else {
			v = v != rhs
		}
	}
	return v, nil
}

func (p *parser) parseUnary() (bool, error) {
	if p.peek() == "!" {
		p.pos++
		v, err := p.parseUnary()
		if err != nil {
			return false, err
		}
		return !v, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (bool, error) {
	tok := p.peek()
	switch tok {
	case "":
		return false, fmt.Errorf("unexpected end of #if expression")
	case "(":
		p.pos++
		v, err := p.parseOr()
		if err != nil {
			return false, err
		}
		if p.peek() != ")" {
			return false, fmt.Errorf("missing ')' in #if expression")
		}
		p.pos++
		return v, nil
	case ")", "&&", "||", "==", "!=", "!":
		return false, fmt.Errorf("unexpected token %q in #if expression", tok)
	case "true":
		p.pos++
		return true, nil
	case "false":
		p.pos++
		return false, nil
	default:
		p.pos++
		return p.symbols[tok], nil
	}
}

// condIdents returns the distinct symbol identifiers used in an expression
// (excluding the true/false literals), in first-seen order.
func condIdents(expr string) ([]string, error) {
	toks, err := tokenize(expr)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var ids []string
	for _, t := range toks {
		if t == "true" || t == "false" || !isIdentChar(t[0]) {
			continue
		}
		if !seen[t] {
			seen[t] = true
			ids = append(ids, t)
		}
	}
	return ids, nil
}

func tokenize(expr string) ([]string, error) {
	var toks []string
	i := 0
	for i < len(expr) {
		c := expr[i]
		switch {
		case c == ' ' || c == '\t':
			i++
		case c == '(' || c == ')':
			toks = append(toks, string(c))
			i++
		case c == '&' && i+1 < len(expr) && expr[i+1] == '&':
			toks = append(toks, "&&")
			i += 2
		case c == '|' && i+1 < len(expr) && expr[i+1] == '|':
			toks = append(toks, "||")
			i += 2
		case c == '=' && i+1 < len(expr) && expr[i+1] == '=':
			toks = append(toks, "==")
			i += 2
		case c == '!' && i+1 < len(expr) && expr[i+1] == '=':
			toks = append(toks, "!=")
			i += 2
		case c == '!':
			toks = append(toks, "!")
			i++
		case isIdentChar(c):
			start := i
			for i < len(expr) && isIdentChar(expr[i]) {
				i++
			}
			toks = append(toks, expr[start:i])
		default:
			return nil, fmt.Errorf("invalid character %q in #if expression", string(c))
		}
	}
	return toks, nil
}
