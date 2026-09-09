package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func main() {
	var (
		inDir   = flag.String("in", "", "input repo/source directory (required)")
		outDir  = flag.String("out", "", "output directory for extracted active code (required)")
		defines stringList
		maybes  stringList
		defCSV  = flag.String("defines", "", "comma-separated ON symbols (in addition to -define)")
		mayCSV  = flag.String("maybes", "", "comma-separated MAYBE symbols (in addition to -maybe)")
		extCSV  = flag.String("ext", ".cs", "comma-separated file extensions to process")
		dryRun  = flag.Bool("dry-run", false, "report actions without writing output")
	)
	flag.Var(&defines, "define", "an ON symbol: resolves conditionals (repeatable), e.g. -define USE_IAP")
	flag.Var(&maybes, "maybe", "a MAYBE symbol: keeps reachable branches guarded (repeatable), e.g. -maybe UNITY_IOS")
	flag.Parse()

	if *inDir == "" || *outDir == "" {
		fmt.Fprintln(os.Stderr, "error: -in and -out are required")
		flag.Usage()
		os.Exit(2)
	}

	model := Model{}
	for _, d := range append(splitCSV(*mayCSV), maybes...) {
		if d = strings.TrimSpace(d); d != "" {
			model[d] = symMaybe
		}
	}
	// ON wins over MAYBE, so apply defines last.
	for _, d := range append(splitCSV(*defCSV), defines...) {
		if d = strings.TrimSpace(d); d != "" {
			model[d] = symOn
		}
	}

	exts := map[string]bool{}
	for _, e := range strings.Split(*extCSV, ",") {
		if e = strings.TrimSpace(e); e != "" {
			if !strings.HasPrefix(e, ".") {
				e = "." + e
			}
			exts[strings.ToLower(e)] = true
		}
	}

	if err := run(*inDir, *outDir, model, exts, *dryRun); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func run(inDir, outDir string, model Model, exts map[string]bool, dryRun bool) error {
	inDir = filepath.Clean(inDir)
	outAbs, err := filepath.Abs(outDir)
	if err != nil {
		return err
	}

	resolver := newAsmResolver(inDir, model)

	var written, skipped, skippedAsm, failed int
	walkErr := filepath.WalkDir(inDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Never descend into the output dir or the VCS metadata.
			abs, _ := filepath.Abs(path)
			if abs == outAbs || d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}

		// Only target extensions are shipped; everything else is dropped.
		if !exts[strings.ToLower(filepath.Ext(path))] {
			skipped++
			return nil
		}

		// Drop the whole file if its assembly (nearest .asmdef) can't compile.
		dead, err := resolver.excluded(filepath.Dir(path))
		if err != nil {
			return err
		}
		if dead {
			skipped++
			skippedAsm++
			return nil
		}

		rel, err := filepath.Rel(inDir, path)
		if err != nil {
			return err
		}

		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result, perr := Strip(string(src), model)
		if perr != nil {
			failed++
			fmt.Fprintf(os.Stderr, "warn: %s: %v (kept unchanged)\n", rel, perr)
			result = string(src)
		}

		// Drop a file left with no real code (only usings, empty namespace,
		// comments, or whitespace after stripping).
		if !hasRealCode(result) {
			skipped++
			return nil
		}

		written++
		if dryRun {
			return nil
		}
		dst := filepath.Join(outDir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, []byte(result), 0o644)
	})
	if walkErr != nil {
		return walkErr
	}

	fmt.Printf("written %d, skipped %d (%d by asmdef), failed %d\n", written, skipped, skippedAsm, failed)
	return nil
}
