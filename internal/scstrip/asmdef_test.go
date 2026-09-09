package scstrip

import "testing"

func TestAsmdefDefineConstraints(t *testing.T) {
	model := Model{"UNITY_IOS": symMaybe, "ENABLE_IL2CPP": symOn}

	// !ENABLE_IL2CPP is false because IL2CPP is ON -> unsatisfiable -> excluded.
	ex, _, _ := assemblyExcluded(&asmdef{DefineConstraints: []string{"!ENABLE_IL2CPP"}}, model)
	if !ex {
		t.Fatal("expected excluded for !ENABLE_IL2CPP when IL2CPP on")
	}
	// UNITY_EDITOR is OFF -> excluded.
	ex, _, _ = assemblyExcluded(&asmdef{DefineConstraints: []string{"UNITY_EDITOR"}}, model)
	if !ex {
		t.Fatal("expected excluded for UNITY_EDITOR constraint when off")
	}
	// UNITY_IOS is MAYBE -> satisfiable -> kept.
	ex, _, _ = assemblyExcluded(&asmdef{DefineConstraints: []string{"UNITY_IOS"}}, model)
	if ex {
		t.Fatal("expected kept for UNITY_IOS maybe constraint")
	}
}

func TestAsmdefPlatforms(t *testing.T) {
	model := Model{"UNITY_IOS": symMaybe} // UNITY_EDITOR off

	// Editor-only assembly, editor off -> excluded.
	ex, _, _ := assemblyExcluded(&asmdef{IncludePlatforms: []string{"Editor"}}, model)
	if !ex {
		t.Fatal("expected editor-only excluded when editor off")
	}
	// includePlatforms iOS with UNITY_IOS maybe -> kept.
	ex, _, _ = assemblyExcluded(&asmdef{IncludePlatforms: []string{"iOS", "Android"}}, model)
	if ex {
		t.Fatal("expected kept when an included platform is maybe")
	}
	// excludePlatforms iOS while iOS is the only target -> excluded.
	ex, _, _ = assemblyExcluded(&asmdef{ExcludePlatforms: []string{"iOS"}}, model)
	if !ex {
		t.Fatal("expected excluded when only target platform is excluded")
	}
}
