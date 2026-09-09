package main

import (
	"fmt"
	"strings"
)

// SymState is the known state of a preprocessor symbol.
type SymState int

const (
	symOff   SymState = iota // definitely undefined (also the zero value for absent symbols)
	symMaybe                 // may be defined; keep every reachable branch, guarded
	symOn                    // definitely defined; conditionals resolve to the taken branch
)

// Model maps symbols to their state. Symbols absent from the map are OFF
// (definitely undefined) and their branches are removed.
type Model map[string]SymState

// maxMaybeSymbols caps the exhaustive reachability search per #if chain (2^n).
const maxMaybeSymbols = 20

// Strip evaluates C# conditional-compilation directives against the model.
//   - ON symbols behave like C# defines: chains resolve to the single taken
//     branch and the directive lines are removed.
//   - MAYBE symbols keep every branch that is reachable under some combination
//     of MAYBE values; the surviving #if/#elif/#else/#endif structure is kept
//     (dead branches pruned) so the output stays valid C#.
//   - OFF symbols (absent) make their branches unreachable, so they are removed.
//
// Original line endings are preserved. #define/#undef and other non-conditional
// directives (#region, #pragma, ...) pass through unchanged and do not affect
// evaluation — drive symbols via the model.
const utf8BOM = "\uFEFF"

func Strip(src string, model Model) (string, error) {
	// A leading BOM is stripped for parsing and re-added to the output so a
	// first-line directive is still recognized and the file's bytes are kept.
	bom := ""
	if strings.HasPrefix(src, utf8BOM) {
		bom = utf8BOM
		src = src[len(utf8BOM):]
	}

	lines := splitLines(src)
	markDirectiveLines(lines)
	p := &nodeParser{lines: lines}
	body, err := p.parseBody()
	if err != nil {
		return "", err
	}
	if p.pos != len(p.lines) {
		kw, _, _ := parseDirective(p.lines[p.pos].text)
		return "", fmt.Errorf("line %d: #%s without #if", p.pos+1, kw)
	}

	body, err = transformNodes(body, model)
	if err != nil {
		return "", err
	}

	var out strings.Builder
	out.WriteString(bom)
	renderNodes(&out, body)
	return out.String(), nil
}

type node interface{}

type textNode struct {
	lines []srcLine
}

type condBranch struct {
	kind   string // "if", "elif", "else"
	cond   string // condition text (empty for else)
	indent string
	nl     string
	body   []node
}

type condNode struct {
	branches    []condBranch
	endifIndent string
	endifNl     string
}

type nodeParser struct {
	lines []srcLine
	pos   int
}

// parseBody reads nodes until EOF or an #elif/#else/#endif at this level
// (which it leaves for the caller to consume).
func (p *nodeParser) parseBody() ([]node, error) {
	var nodes []node
	var text []srcLine
	flush := func() {
		if len(text) > 0 {
			nodes = append(nodes, &textNode{lines: text})
			text = nil
		}
	}

	for p.pos < len(p.lines) {
		ln := p.lines[p.pos]
		if kw, _, ok := parseDirective(ln.text); ok && ln.directive {
			switch kw {
			case "if":
				flush()
				cn, err := p.parseCond()
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, cn)
				continue
			case "elif", "else", "endif":
				flush()
				return nodes, nil
			}
		}
		text = append(text, ln)
		p.pos++
	}
	flush()
	return nodes, nil
}

// parseCond parses a full #if..#endif group; p.pos must be at the #if.
func (p *nodeParser) parseCond() (*condNode, error) {
	cn := &condNode{}
	for {
		if p.pos >= len(p.lines) {
			return nil, fmt.Errorf("unbalanced directives: missing #endif")
		}
		ln := p.lines[p.pos]
		kw, arg, _ := parseDirective(ln.text)
		switch kw {
		case "if", "elif", "else":
			p.pos++
			body, err := p.parseBody()
			if err != nil {
				return nil, err
			}
			br := condBranch{kind: kw, indent: leadingWS(ln.text), nl: ln.nl, body: body}
			if kw != "else" {
				br.cond = arg
			}
			cn.branches = append(cn.branches, br)
		case "endif":
			cn.endifIndent = leadingWS(ln.text)
			cn.endifNl = ln.nl
			p.pos++
			return cn, nil
		default:
			return nil, fmt.Errorf("line %d: unexpected #%s in #if group", p.pos+1, kw)
		}
	}
}

// transformNodes threads the symbol model through nodes in document order so
// in-file #define/#undef affect the evaluation of everything after them. Defines
// inside a kept MAYBE branch stay local to that branch (handled by recursion);
// defines from an inlined definite branch surface here and carry forward.
func transformNodes(nodes []node, model Model) ([]node, error) {
	cur := cloneModel(model)
	var out []node
	for _, n := range nodes {
		switch t := n.(type) {
		case *textNode:
			applyDefines(t, cur)
			out = append(out, t)
		case *condNode:
			res, err := transformCond(t, cur)
			if err != nil {
				return nil, err
			}
			for _, rn := range res {
				if tn, ok := rn.(*textNode); ok {
					applyDefines(tn, cur)
				}
			}
			out = append(out, res...)
		}
	}
	return out, nil
}

func cloneModel(m Model) Model {
	c := make(Model, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

// applyDefines updates model in place for #define/#undef found in a text block.
func applyDefines(t *textNode, model Model) {
	for _, ln := range t.lines {
		kw, arg, ok := parseDirective(ln.text)
		if !ok || !ln.directive {
			continue
		}
		sym := firstToken(arg)
		if sym == "" {
			continue
		}
		switch kw {
		case "define":
			model[sym] = symOn
		case "undef":
			delete(model, sym)
		}
	}
}

func firstToken(s string) string {
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i]
	}
	return s
}

// transformCond prunes unreachable branches. If exactly one branch is always
// taken it is inlined (directives dropped); otherwise the reachable branches are
// kept with their directive structure rebuilt.
func transformCond(cn *condNode, model Model) ([]node, error) {
	maybeSyms, err := chainMaybeSymbols(cn, model)
	if err != nil {
		return nil, err
	}
	if len(maybeSyms) > maxMaybeSymbols {
		return nil, fmt.Errorf("too many 'maybe' symbols (%d) in one #if chain", len(maybeSyms))
	}

	reachable := map[int]bool{}
	activeValues := map[int]bool{}
	for mask := 0; mask < (1 << len(maybeSyms)); mask++ {
		syms := baseSymbols(model)
		for i, s := range maybeSyms {
			if mask&(1<<i) != 0 {
				syms[s] = true
			}
		}
		active, err := activeBranch(cn, syms)
		if err != nil {
			return nil, err
		}
		activeValues[active] = true
		if active >= 0 {
			reachable[active] = true
		}
	}

	// A single active outcome across every assignment means the branch (or the
	// "nothing" outcome) is definite.
	if len(activeValues) == 1 {
		only := -1
		for k := range activeValues {
			only = k
		}
		if only < 0 {
			return nil, nil
		}
		return transformNodes(cn.branches[only].body, model)
	}

	var kept []condBranch
	for i, br := range cn.branches {
		if !reachable[i] {
			continue
		}
		tb, err := transformNodes(br.body, model)
		if err != nil {
			return nil, err
		}
		br.body = tb
		kept = append(kept, br)
	}
	if len(kept) == 0 {
		return nil, nil
	}
	// The first surviving branch must open the chain as #if; later conditional
	// survivors become #elif. (An #else can never be the first survivor here:
	// that would mean it is always taken, handled by the definite case above.)
	if kept[0].kind == "else" {
		return kept[0].body, nil
	}
	for i := range kept {
		switch {
		case i == 0:
			kept[i].kind = "if"
		case kept[i].kind != "else":
			kept[i].kind = "elif"
		}
	}
	return []node{&condNode{branches: kept, endifIndent: cn.endifIndent, endifNl: cn.endifNl}}, nil
}

// activeBranch returns the index of the branch taken under syms, or -1 if none.
func activeBranch(cn *condNode, syms map[string]bool) (int, error) {
	for i, br := range cn.branches {
		if br.kind == "else" {
			return i, nil
		}
		v, err := evalExpr(br.cond, syms)
		if err != nil {
			return 0, err
		}
		if v {
			return i, nil
		}
	}
	return -1, nil
}

func chainMaybeSymbols(cn *condNode, model Model) ([]string, error) {
	seen := map[string]bool{}
	var syms []string
	for _, br := range cn.branches {
		if br.kind == "else" {
			continue
		}
		ids, err := condIdents(br.cond)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if model[id] == symMaybe && !seen[id] {
				seen[id] = true
				syms = append(syms, id)
			}
		}
	}
	return syms, nil
}

func baseSymbols(model Model) map[string]bool {
	m := map[string]bool{}
	for k, v := range model {
		if v == symOn {
			m[k] = true
		}
	}
	return m
}

func renderNodes(out *strings.Builder, nodes []node) {
	for _, n := range nodes {
		switch t := n.(type) {
		case *textNode:
			for _, ln := range t.lines {
				out.WriteString(ln.text)
				out.WriteString(ln.nl)
			}
		case *condNode:
			for _, br := range t.branches {
				out.WriteString(br.indent)
				out.WriteString("#")
				out.WriteString(br.kind)
				if br.kind != "else" && br.cond != "" {
					out.WriteString(" ")
					out.WriteString(br.cond)
				}
				out.WriteString(br.nl)
				renderNodes(out, br.body)
			}
			out.WriteString(t.endifIndent)
			out.WriteString("#endif")
			out.WriteString(t.endifNl)
		}
	}
}

type srcLine struct {
	text      string // line content without the trailing newline
	nl        string // "\n", "\r\n", or "" for a final line without newline
	directive bool   // line starts in code context, so a leading # is a real directive
}

// markDirectiveLines flags each line that starts in code context (i.e. not
// inside a /* */ block comment or a multi-line verbatim string). Only such
// lines may be preprocessor directives; a #if/#endif written inside a comment
// or string must be treated as plain text.
func markDirectiveLines(lines []srcLine) {
	inBlock, inVerbatim := false, false
	for i := range lines {
		lines[i].directive = !inBlock && !inVerbatim
		inBlock, inVerbatim = scanLineState(lines[i].text, inBlock, inVerbatim)
	}
}

// scanLineState returns the comment/string state at the end of a line given the
// state at its start. It handles // and /* */ comments, regular and char
// literals (with \ escapes), and @-verbatim strings ("" escapes) that may span
// lines. Interpolation braces are not modeled, which is immaterial to whether a
// following line begins in code.
func scanLineState(text string, inBlock, inVerbatim bool) (bool, bool) {
	i, n := 0, len(text)
	if inVerbatim {
		for i < n {
			if text[i] == '"' {
				if i+1 < n && text[i+1] == '"' {
					i += 2
					continue
				}
				i++
				inVerbatim = false
				break
			}
			i++
		}
		if inVerbatim {
			return false, true
		}
	}
	if inBlock {
		if j := strings.Index(text[i:], "*/"); j >= 0 {
			i += j + 2
			inBlock = false
		} else {
			return true, false
		}
	}
	for i < n {
		switch {
		case text[i] == '/' && i+1 < n && text[i+1] == '/':
			return false, false
		case text[i] == '/' && i+1 < n && text[i+1] == '*':
			if j := strings.Index(text[i+2:], "*/"); j >= 0 {
				i += 2 + j + 2
			} else {
				return true, false
			}
		case text[i] == '"' && i > 0 && text[i-1] == '@':
			i++
			closed := false
			for i < n {
				if text[i] == '"' {
					if i+1 < n && text[i+1] == '"' {
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				i++
			}
			if !closed {
				return false, true
			}
		case text[i] == '"':
			i++
			for i < n {
				if text[i] == '\\' {
					i += 2
					continue
				}
				if text[i] == '"' {
					i++
					break
				}
				i++
			}
		case text[i] == '\'':
			i++
			for i < n {
				if text[i] == '\\' {
					i += 2
					continue
				}
				if text[i] == '\'' {
					i++
					break
				}
				i++
			}
		default:
			i++
		}
	}
	return false, false
}

// splitLines splits src into lines, preserving each line's original terminator.
func splitLines(src string) []srcLine {
	var lines []srcLine
	i := 0
	for i < len(src) {
		j := strings.IndexByte(src[i:], '\n')
		if j < 0 {
			lines = append(lines, srcLine{text: src[i:], nl: ""})
			break
		}
		content := src[i : i+j]
		nl := "\n"
		if strings.HasSuffix(content, "\r") {
			content = content[:len(content)-1]
			nl = "\r\n"
		}
		lines = append(lines, srcLine{text: content, nl: nl})
		i += j + 1
	}
	return lines
}

// parseDirective reports whether text is a preprocessor directive and, if so,
// returns its keyword and argument (with any trailing comment stripped).
func parseDirective(text string) (kw, arg string, ok bool) {
	t := strings.TrimLeft(text, " \t"+utf8BOM)
	if !strings.HasPrefix(t, "#") {
		return "", "", false
	}
	t = strings.TrimLeft(t[1:], " \t")

	end := 0
	for end < len(t) && isIdentChar(t[end]) {
		end++
	}
	if end == 0 {
		return "", "", false
	}
	kw = t[:end]
	arg = stripComment(strings.TrimSpace(t[end:]))
	return kw, arg, true
}

func stripComment(s string) string {
	if i := strings.Index(s, "//"); i >= 0 {
		s = s[:i]
	}
	if i := strings.Index(s, "/*"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func leadingWS(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return s[:i]
}

func isIdentChar(c byte) bool {
	return c == '_' ||
		(c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9')
}
