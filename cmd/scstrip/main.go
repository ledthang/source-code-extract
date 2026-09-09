// Command scstrip extracts the C# code that would actually build under a given
// set of preprocessor symbols, dropping everything the compiler wouldn't reach.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ledthang/source-code-extract/internal/scstrip"
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

	model := scstrip.Model{}
	for _, d := range append(splitCSV(*mayCSV), maybes...) {
		if d = strings.TrimSpace(d); d != "" {
			model.SetMaybe(d)
		}
	}
	for _, d := range append(splitCSV(*defCSV), defines...) {
		if d = strings.TrimSpace(d); d != "" {
			model.SetOn(d)
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

	stats, err := scstrip.Run(*inDir, *outDir, model, exts, *dryRun)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Printf("written %d, skipped %d (%d by asmdef), failed %d\n",
		stats.Written, stats.Skipped, stats.SkippedAsm, stats.Failed)
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return strings.Split(s, ",")
}
