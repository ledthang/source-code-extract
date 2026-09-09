package main

import "testing"

// on strips with the given symbols all marked ON (definite).
func on(t *testing.T, src string, defs ...string) string {
	t.Helper()
	m := Model{}
	for _, d := range defs {
		m[d] = symOn
	}
	out, err := Strip(src, m)
	if err != nil {
		t.Fatalf("Strip error: %v", err)
	}
	return out
}

func stripM(t *testing.T, src string, m Model) string {
	t.Helper()
	out, err := Strip(src, m)
	if err != nil {
		t.Fatalf("Strip error: %v", err)
	}
	return out
}

func TestOn_UserExampleKept(t *testing.T) {
	src := "before\n#if USE_IAP\n    body\n    else\n#endif\nafter\n"
	if got := on(t, src, "USE_IAP"); got != "before\n    body\n    else\nafter\n" {
		t.Fatalf("kept: got %q", got)
	}
	if got := on(t, src); got != "before\nafter\n" {
		t.Fatalf("removed: got %q", got)
	}
}

func TestOn_ElifElse(t *testing.T) {
	src := "#if A\na\n#elif B\nb\n#else\nc\n#endif\n"
	if got := on(t, src, "B"); got != "b\n" {
		t.Fatalf("elif: got %q", got)
	}
	if got := on(t, src); got != "c\n" {
		t.Fatalf("else: got %q", got)
	}
	if got := on(t, src, "A"); got != "a\n" {
		t.Fatalf("if: got %q", got)
	}
}

func TestOn_Expressions(t *testing.T) {
	cases := []struct {
		expr string
		defs []string
		want string
	}{
		{"A && B", []string{"A", "B"}, "x\n"},
		{"A && B", []string{"A"}, ""},
		{"A || B", []string{"B"}, "x\n"},
		{"!A", nil, "x\n"},
		{"(A || B) && !C", []string{"A"}, "x\n"},
		{"(A || B) && !C", []string{"A", "C"}, ""},
		{"UNITY_IOS && USE_BG && !UNITY_EDITOR", []string{"UNITY_IOS", "USE_BG"}, "x\n"},
		{"UNITY_IOS && USE_BG && !UNITY_EDITOR", []string{"UNITY_IOS", "USE_BG", "UNITY_EDITOR"}, ""},
	}
	for _, c := range cases {
		src := "#if " + c.expr + "\nx\n#endif\n"
		if got := on(t, src, c.defs...); got != c.want {
			t.Fatalf("expr %q defs %v: got %q want %q", c.expr, c.defs, got, c.want)
		}
	}
}

// The core new behavior: multiple platform symbols marked MAYBE keep every
// reachable branch, guarded, while OFF platforms are pruned.
func TestMaybe_PlatformChainKeepsReachable(t *testing.T) {
	src := "" +
		"#if UNITY_STANDALONE_WIN\n    win\n" +
		"#elif UNITY_STANDALONE_OSX\n    osx\n" +
		"#elif UNITY_IOS\n    ios\n" +
		"#elif UNITY_PS4 || UNITY_PS5\n    ps\n" +
		"#else\n    other\n" +
		"#endif\n"
	m := Model{"UNITY_IOS": symMaybe, "UNITY_STANDALONE_OSX": symMaybe}
	want := "" +
		"#if UNITY_STANDALONE_OSX\n    osx\n" +
		"#elif UNITY_IOS\n    ios\n" +
		"#else\n    other\n" +
		"#endif\n"
	if got := stripM(t, src, m); got != want {
		t.Fatalf("maybe chain:\n got=%q\nwant=%q", got, want)
	}
}

func TestMaybe_SingleFlagKeepsGuard(t *testing.T) {
	src := "#if USE_IAP\niap\n#endif\ntail\n"
	m := Model{"USE_IAP": symMaybe}
	if got := stripM(t, src, m); got != src {
		t.Fatalf("maybe single: got %q", got)
	}
}

func TestMaybe_OnResolvesMaybeKeeps(t *testing.T) {
	src := "#if A\na\n#else\nb\n#endif\n"
	if got := stripM(t, src, Model{"A": symOn}); got != "a\n" {
		t.Fatalf("on resolves: got %q", got)
	}
	if got := stripM(t, src, Model{"A": symMaybe}); got != src {
		t.Fatalf("maybe keeps both: got %q", got)
	}
}

// OFF branches inside a kept MAYBE chain are pruned; the guard collapses to #if.
func TestMaybe_PrunesOffBranchToIf(t *testing.T) {
	src := "#if WIN\nwin\n#elif OSX\nosx\n#endif\n"
	m := Model{"OSX": symMaybe} // WIN is OFF
	want := "#if OSX\nosx\n#endif\n"
	if got := stripM(t, src, m); got != want {
		t.Fatalf("prune off: got %q want %q", got, want)
	}
}

func TestMaybe_NestedInsideKeptBranch(t *testing.T) {
	src := "#if OSX\nosx\n#if USE_IAP\niap\n#endif\n#elif WIN\nwin\n#endif\n"
	m := Model{"OSX": symMaybe, "USE_IAP": symOn} // WIN OFF
	want := "#if OSX\nosx\niap\n#endif\n"
	if got := stripM(t, src, m); got != want {
		t.Fatalf("nested: got %q want %q", got, want)
	}
}

func TestHonorInFileDefine(t *testing.T) {
	src := "#define FOO\n#if FOO\nyes\n#endif\n#undef FOO\n#if FOO\nno\n#endif\n"
	want := "#define FOO\nyes\n#undef FOO\n"
	if got := on(t, src); got != want {
		t.Fatalf("in-file define/undef: got %q want %q", got, want)
	}
}

func TestConditionalDefineFromOnBranchCarries(t *testing.T) {
	// TRACE is defined only when DEBUG is ON; the definite branch is inlined and
	// the #define carries forward to the following chain.
	src := "#if DEBUG\n#define TRACE\n#endif\n#if TRACE\ntrace\n#endif\n"
	if got := on(t, src, "DEBUG"); got != "#define TRACE\ntrace\n" {
		t.Fatalf("carry with DEBUG on: got %q", got)
	}
	if got := on(t, src); got != "" {
		t.Fatalf("no DEBUG so no TRACE: got %q", got)
	}
}

func TestDefineInsideMaybeBranchDoesNotLeak(t *testing.T) {
	// FOO is defined only inside a MAYBE branch, so the later #if FOO must not be
	// treated as definitely ON at the outer level.
	src := "#if PLAT\n#define FOO\n#endif\n#if FOO\nfoo\n#endif\n"
	m := Model{"PLAT": symMaybe}
	want := "#if PLAT\n#define FOO\n#endif\n"
	if got := stripM(t, src, m); got != want {
		t.Fatalf("leak: got %q want %q", got, want)
	}
}

func TestLeadingBOMDirective(t *testing.T) {
	bom := "\uFEFF"
	src := bom + "#if UNITY_EDITOR\r\nusing UnityEditor;\r\n#endif\r\nusing System;\r\n"
	// UNITY_EDITOR OFF: block removed, BOM preserved on the surviving first line.
	if got := on(t, src); got != bom+"using System;\r\n" {
		t.Fatalf("bom removed-branch: got %q", got)
	}
	// UNITY_EDITOR ON: inner kept, BOM preserved.
	if got := on(t, src, "UNITY_EDITOR"); got != bom+"using UnityEditor;\r\nusing System;\r\n" {
		t.Fatalf("bom kept-branch: got %q", got)
	}
}

func TestCRLFPreserved(t *testing.T) {
	src := "a\r\n#if X\r\nb\r\n#endif\r\nc\r\n"
	if got := on(t, src, "X"); got != "a\r\nb\r\nc\r\n" {
		t.Fatalf("crlf kept: got %q", got)
	}
	if got := on(t, src); got != "a\r\nc\r\n" {
		t.Fatalf("crlf removed: got %q", got)
	}
}

func TestNonConditionalDirectivesKept(t *testing.T) {
	src := "#region R\n#define FOO\n#pragma warning disable\ncode\n#endregion\n"
	if got := on(t, src); got != src {
		t.Fatalf("non-conditional directive changed: got %q", got)
	}
}

func TestTrailingCommentAndWhitespace(t *testing.T) {
	src := "  #if  A   // enable A\nx\n  #endif // done\n"
	if got := on(t, src, "A"); got != "x\n" {
		t.Fatalf("trailing comment: got %q", got)
	}
}

func TestDirectivesInBlockCommentIgnored(t *testing.T) {
	// A #if/#elif/#endif written inside /* */ is commented-out code, not a
	// directive, and must pass through untouched.
	src := "" +
		"/*\n#if UBISOFT_DEBUG\ndbg\n#elif UBISOFT_RETAIL\nret\n*/\n" +
		"#if UNITY_EDITOR\ned\n#endif\ntail\n"
	if got := on(t, src); got != "/*\n#if UBISOFT_DEBUG\ndbg\n#elif UBISOFT_RETAIL\nret\n*/\ntail\n" {
		t.Fatalf("block-comment directives: got %q", got)
	}
}

func TestClosingEndifInBlockCommentIgnored(t *testing.T) {
	// Whole guarded block is inside the comment; the opening /*#if is not a
	// directive and the closing #endif*/ must not be seen as a stray #endif.
	src := "keep\n/*#if! ALKAWA_FINAL\ndead\n#endif*/\nafter\n"
	if got := on(t, src); got != src {
		t.Fatalf("commented endif: got %q", got)
	}
}

func TestDirectiveInVerbatimStringIgnored(t *testing.T) {
	src := "var s = @\"\n#if X\n\";\n#if Y\ny\n#endif\n"
	if got := on(t, src, "Y"); got != "var s = @\"\n#if X\n\";\ny\n" {
		t.Fatalf("verbatim string: got %q", got)
	}
}

func TestUnbalanced(t *testing.T) {
	if _, err := Strip("#if A\nx\n", Model{}); err == nil {
		t.Fatal("expected missing #endif error")
	}
	if _, err := Strip("x\n#endif\n", Model{}); err == nil {
		t.Fatal("expected stray #endif error")
	}
}
