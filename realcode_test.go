package main

import "testing"

func TestHasRealCode(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{"bom only", "\uFEFF", false},
		{"bom whitespace", "\uFEFF\r\n\r\n", false},
		{"usings only", "\uFEFFusing System;\r\nusing UnityEditor;\r\n", false},
		{"global using", "global using System;\n", false},
		{"empty namespace", "using System;\nnamespace Foo {\n}\n", false},
		{"file-scoped empty namespace", "namespace Foo.Bar;\n", false},
		{"only comments", "// gone\n/* also\n gone */\n", false},
		{"doc comment leftover", "using System;\nnamespace Foo {\n    /// <summary>x</summary>\n}\n", false},
		{"real class", "using System;\nnamespace Foo {\n    public class Bar {}\n}\n", true},
		{"enum", "namespace Foo {\npublic enum E { A }\n}\n", true},
		{"assembly attribute", "[assembly: System.Reflection.AssemblyVersion(\"1.0\")]\n", true},
		{"comment with slashes in string kept", "var s = \"//not a comment\";\n", true},
	}
	for _, c := range cases {
		if got := hasRealCode(c.src); got != c.want {
			t.Errorf("%s: hasRealCode=%v want %v", c.name, got, c.want)
		}
	}
}
