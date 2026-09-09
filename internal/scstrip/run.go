package scstrip

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Stats summarizes the outcome of a Run.
type Stats struct {
	Written    int // files with real code, written to outDir (or would be, in a dry run)
	Skipped    int // files dropped: wrong extension, dead asmdef, or no real code left
	SkippedAsm int // subset of Skipped dropped because their assembly can't compile
	Failed     int // files with unbalanced directives, kept unchanged and copied as-is
}

// Run walks inDir, strips conditional-compilation code from files matching an
// extension in exts against model, and writes the survivors under outDir
// (mirroring their relative path). Files whose assembly (nearest .asmdef)
// can't compile under model, and files left with no real code after
// stripping, are dropped. The output directory and .git are skipped when
// walking. If dryRun is true, nothing is written.
func Run(inDir, outDir string, model Model, exts map[string]bool, dryRun bool) (Stats, error) {
	inDir = filepath.Clean(inDir)
	outAbs, err := filepath.Abs(outDir)
	if err != nil {
		return Stats{}, err
	}

	resolver := newAsmResolver(inDir, model)

	var stats Stats
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
			stats.Skipped++
			return nil
		}

		// Drop the whole file if its assembly (nearest .asmdef) can't compile.
		dead, err := resolver.excluded(filepath.Dir(path))
		if err != nil {
			return err
		}
		if dead {
			stats.Skipped++
			stats.SkippedAsm++
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
			stats.Failed++
			fmt.Fprintf(os.Stderr, "warn: %s: %v (kept unchanged)\n", rel, perr)
			result = string(src)
		}

		// Drop a file left with no real code (only usings, empty namespace,
		// comments, or whitespace after stripping).
		if !hasRealCode(result) {
			stats.Skipped++
			return nil
		}

		stats.Written++
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
		return stats, walkErr
	}
	return stats, nil
}
