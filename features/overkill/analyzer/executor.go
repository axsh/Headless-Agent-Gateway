package analyzer

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
)

// Execute removes dead code from source files.
// Returns the number of symbols removed.
func Execute(result *AnalysisResult) (int, error) {
	removed := 0

	// 1. Delete dead files first (all symbols in these files are dead).
	for _, df := range result.DeadFiles {
		if err := os.Remove(df.File); err != nil {
			return removed, fmt.Errorf("remove dead file %s: %w", df.File, err)
		}
		removed += len(df.Symbols)
	}

	// 2. Remove individual dead symbols from remaining files.
	if len(result.DeadSymbols) == 0 {
		return removed, nil
	}

	// Group dead symbols by file.
	fileSymbols := make(map[string][]SymbolDef)
	for _, def := range result.DeadSymbols {
		fileSymbols[def.File] = append(fileSymbols[def.File], def)
	}

	for filePath, defs := range fileSymbols {
		source, err := os.ReadFile(filePath)
		if err != nil {
			return removed, fmt.Errorf("read %s: %w", filePath, err)
		}

		// Sort removals in reverse byte order to preserve offsets.
		sort.Slice(defs, func(i, j int) bool {
			return defs[i].StartByte > defs[j].StartByte
		})

		// Remove each dead symbol from the source.
		modified := source
		for _, def := range defs {
			start := def.StartByte
			end := def.EndByte
			if start >= uint(len(modified)) || end > uint(len(modified)) {
				continue
			}
			modified = append(modified[:start], modified[end:]...)
			removed++
		}

		// Write back modified content.
		if err := os.WriteFile(filePath, modified, 0o644); err != nil {
			return removed, fmt.Errorf("write %s: %w", filePath, err)
		}

		// Run gofmt to clean up formatting.
		cmd := exec.Command("gofmt", "-w", filePath)
		if err := cmd.Run(); err != nil {
			// gofmt failure is not fatal; the file may have syntax issues.
			fmt.Fprintf(os.Stderr, "Warning: gofmt failed on %s: %v\n", filePath, err)
		}
	}

	return removed, nil
}
