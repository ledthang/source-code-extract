package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// asmdef holds the fields of a Unity .asmdef we act on.
type asmdef struct {
	Name              string   `json:"name"`
	DefineConstraints []string `json:"defineConstraints"`
	IncludePlatforms  []string `json:"includePlatforms"`
	ExcludePlatforms  []string `json:"excludePlatforms"`
}

// platformDefine maps Unity asmdef platform names to their build define symbol.
var platformDefine = map[string]string{
	"Editor":              "UNITY_EDITOR",
	"iOS":                 "UNITY_IOS",
	"Android":             "UNITY_ANDROID",
	"WindowsStandalone32": "UNITY_STANDALONE_WIN",
	"WindowsStandalone64": "UNITY_STANDALONE_WIN",
	"macOSStandalone":     "UNITY_STANDALONE_OSX",
	"LinuxStandalone64":   "UNITY_STANDALONE_LINUX",
	"WSA":                 "UNITY_WSA",
	"PS4":                 "UNITY_PS4",
	"PS5":                 "UNITY_PS5",
	"XboxOne":             "UNITY_XBOXONE",
	"Switch":              "UNITY_SWITCH",
	"tvOS":                "UNITY_TVOS",
	"WebGL":               "UNITY_WEBGL",
	"Lumin":               "UNITY_LUMIN",
	"Stadia":              "UNITY_STADIA",
}

// assemblyExcluded reports whether an assembly definitely does not compile under
// the model, and why. defineConstraints must be satisfiable and the target
// platforms must overlap the assembly's platform set; otherwise it is dead.
func assemblyExcluded(asm *asmdef, model Model) (bool, string, error) {
	if len(asm.DefineConstraints) > 0 {
		expr := "(" + strings.Join(asm.DefineConstraints, ") && (") + ")"
		sat, err := satisfiable(expr, model)
		if err != nil {
			return false, "", err
		}
		if !sat {
			return true, "defineConstraints " + strings.Join(asm.DefineConstraints, ", "), nil
		}
	}

	if len(asm.IncludePlatforms) > 0 {
		for _, p := range asm.IncludePlatforms {
			sym, known := platformDefine[p]
			if !known || model[sym] != symOff {
				return false, "", nil // this platform may be built: keep
			}
		}
		return true, "includePlatforms " + strings.Join(asm.IncludePlatforms, ", "), nil
	}

	if len(asm.ExcludePlatforms) > 0 {
		excluded := map[string]bool{}
		for _, p := range asm.ExcludePlatforms {
			if sym, known := platformDefine[p]; known {
				excluded[sym] = true
			}
		}
		var targets []string
		for _, sym := range platformDefine {
			if model[sym] != symOff {
				targets = append(targets, sym)
			}
		}
		if len(targets) > 0 {
			for _, s := range targets {
				if !excluded[s] {
					return false, "", nil // building a non-excluded platform: keep
				}
			}
			return true, "excludePlatforms " + strings.Join(asm.ExcludePlatforms, ", "), nil
		}
	}

	return false, "", nil
}

// satisfiable reports whether expr can be true under some assignment of its
// MAYBE symbols (ON/OFF symbols are fixed by the model).
func satisfiable(expr string, model Model) (bool, error) {
	ids, err := condIdents(expr)
	if err != nil {
		return false, err
	}
	var maybes []string
	for _, id := range ids {
		if model[id] == symMaybe {
			maybes = append(maybes, id)
		}
	}
	if len(maybes) > maxMaybeSymbols {
		return true, nil
	}
	for mask := 0; mask < (1 << len(maybes)); mask++ {
		syms := baseSymbols(model)
		for i, s := range maybes {
			if mask&(1<<i) != 0 {
				syms[s] = true
			}
		}
		v, err := evalExpr(expr, syms)
		if err != nil {
			return false, err
		}
		if v {
			return true, nil
		}
	}
	return false, nil
}

// asmResolver decides, for a file's directory, whether the assembly that owns it
// is excluded. The nearest ancestor .asmdef defines the assembly; files with no
// ancestor .asmdef belong to the predefined assembly and are always kept.
type asmResolver struct {
	root  string
	model Model
	cache map[string]bool
}

func newAsmResolver(root string, model Model) *asmResolver {
	abs, _ := filepath.Abs(root)
	return &asmResolver{root: filepath.Clean(abs), model: model, cache: map[string]bool{}}
}

func (r *asmResolver) excluded(dir string) (bool, error) {
	dir = filepath.Clean(dir)
	if v, ok := r.cache[dir]; ok {
		return v, nil
	}

	asm, err := findAsmdef(dir)
	if err != nil {
		return false, err
	}

	var result bool
	switch {
	case asm != nil:
		reason := ""
		result, reason, err = assemblyExcluded(asm, r.model)
		if err != nil {
			return false, err
		}
		if result {
			fmt.Fprintf(os.Stderr, "asmdef: skipping assembly %q (%s)\n", asm.Name, reason)
		}
	case dir == r.root || len(dir) <= len(r.root):
		result = false
	default:
		result, err = r.excluded(filepath.Dir(dir))
		if err != nil {
			return false, err
		}
	}

	r.cache[dir] = result
	return result, nil
}

// findAsmdef returns the .asmdef in dir, or nil if none. A malformed file is
// treated as an empty (unconstrained) assembly so its folder is kept.
func findAsmdef(dir string) (*asmdef, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".asmdef") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var asm asmdef
		if err := json.Unmarshal(data, &asm); err != nil {
			fmt.Fprintf(os.Stderr, "warn: %s: invalid asmdef: %v (kept)\n", e.Name(), err)
			return &asmdef{Name: e.Name()}, nil
		}
		return &asm, nil
	}
	return nil, nil
}
