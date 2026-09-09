package main

import "strings"

// hasRealCode reports whether src contains any real declaration after stripping.
// A file left with only a BOM, whitespace, comments, using/extern-alias
// directives, and empty namespace wrappers has no active code and is dropped.
func hasRealCode(src string) bool {
	src = strings.TrimPrefix(src, utf8BOM)
	for _, raw := range strings.Split(stripComments(src), "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		switch {
		case line == "", line == "{", line == "}":
		case strings.HasPrefix(line, "#"):
		case isUsingOrExtern(line):
		case isNamespaceHeader(line):
		default:
			return true
		}
	}
	return false
}

func isUsingOrExtern(line string) bool {
	l := strings.TrimSpace(strings.TrimPrefix(line, "global "))
	if strings.HasPrefix(l, "using ") || strings.HasPrefix(l, "using\t") {
		return !strings.Contains(l, "(") && strings.HasSuffix(l, ";")
	}
	if strings.HasPrefix(l, "extern ") {
		return strings.Contains(l, "alias") && strings.HasSuffix(l, ";")
	}
	return false
}

// isNamespaceHeader matches "namespace X", "namespace X {" and file-scoped
// "namespace X;" (nothing but the declaration on the line).
func isNamespaceHeader(line string) bool {
	rest, ok := strings.CutPrefix(line, "namespace")
	if !ok || rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
		return false
	}
	rest = strings.TrimSpace(rest)
	rest = strings.TrimSpace(strings.TrimSuffix(rest, "{"))
	rest = strings.TrimSpace(strings.TrimSuffix(rest, ";"))
	return isDottedIdent(rest)
}

func isDottedIdent(s string) bool {
	if s == "" {
		return false
	}
	for _, part := range strings.Split(s, ".") {
		if part == "" {
			return false
		}
		for i := 0; i < len(part); i++ {
			if !isIdentChar(part[i]) {
				return false
			}
		}
	}
	return true
}

// stripComments removes // and /* */ comments while preserving newlines and the
// contents of string, verbatim-string, and char literals.
func stripComments(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	n := len(s)
	inBlock, inVerbatim := false, false
	for i := 0; i < n; {
		c := s[i]
		switch {
		case inBlock:
			if c == '*' && i+1 < n && s[i+1] == '/' {
				inBlock = false
				i += 2
				continue
			}
			if c == '\n' {
				b.WriteByte(c)
			}
			i++
		case inVerbatim:
			b.WriteByte(c)
			if c == '"' {
				if i+1 < n && s[i+1] == '"' {
					b.WriteByte('"')
					i += 2
					continue
				}
				inVerbatim = false
			}
			i++
		case c == '/' && i+1 < n && s[i+1] == '/':
			for i < n && s[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && s[i+1] == '*':
			inBlock = true
			i += 2
		case c == '@' && i+1 < n && s[i+1] == '"':
			b.WriteString("@\"")
			inVerbatim = true
			i += 2
		case c == '"' || c == '\'':
			quote := c
			b.WriteByte(c)
			i++
			for i < n {
				b.WriteByte(s[i])
				if s[i] == '\\' && i+1 < n {
					b.WriteByte(s[i+1])
					i += 2
					continue
				}
				if s[i] == quote {
					i++
					break
				}
				i++
			}
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}
