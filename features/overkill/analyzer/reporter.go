package analyzer

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// ReportText outputs dead code report in human-readable format.
func ReportText(w io.Writer, result *AnalysisResult, verbose bool) {
	hasDeadFiles := len(result.DeadFiles) > 0
	hasDeadSymbols := len(result.DeadSymbols) > 0

	if !hasDeadFiles && !hasDeadSymbols {
		fmt.Fprintln(w, "No dead code found.")
		return
	}

	// Dead file report section.
	if hasDeadFiles {
		fmt.Fprintln(w, "=== Dead File Report ===")
		fmt.Fprintln(w)

		for _, df := range result.DeadFiles {
			fmt.Fprintf(w, "DEAD FILE  %s  (%d dead symbols)\n", df.File, len(df.Symbols))
			for _, sym := range df.Symbols {
				name := sym.Name
				if sym.Receiver != "" {
					name = sym.Receiver + "." + sym.Name
				}
				fmt.Fprintf(w, "  - %-7s %s\n", sym.Kind, name)
			}
			fmt.Fprintln(w)
		}
	}

	// Dead symbol report section (excluding dead file members).
	if hasDeadSymbols {
		fmt.Fprintln(w, "=== Dead Code Report ===")
		fmt.Fprintln(w)

		// Group by package.
		groups := groupByPackage(result.DeadSymbols)

		// Sort package names.
		pkgNames := make([]string, 0, len(groups))
		for pkg := range groups {
			pkgNames = append(pkgNames, pkg)
		}
		sort.Strings(pkgNames)

		for _, pkg := range pkgNames {
			defs := groups[pkg]
			// Sort by file and line.
			sort.Slice(defs, func(i, j int) bool {
				if defs[i].File != defs[j].File {
					return defs[i].File < defs[j].File
				}
				return defs[i].Line < defs[j].Line
			})

			fmt.Fprintf(w, "Package: %s\n", pkg)
			for _, def := range defs {
				name := def.Name
				if def.Receiver != "" {
					name = def.Receiver + "." + def.Name
				}
				fmt.Fprintf(w, "  DEAD %-7s %-30s %s:%d\n", def.Kind, name, def.File, def.Line)
			}
			fmt.Fprintln(w)
		}
	}

	// Summary.
	totalDeadSymbols := len(result.DeadSymbols)
	for _, df := range result.DeadFiles {
		totalDeadSymbols += len(df.Symbols)
	}
	groups := groupByPackage(result.DeadSymbols)
	fmt.Fprintf(w, "Summary: %d dead files, %d dead symbols found\n",
		len(result.DeadFiles), totalDeadSymbols)

	if verbose {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Total definitions: %d\n", len(result.AllDefs))
		fmt.Fprintf(w, "Total references:  %d\n", len(result.AllRefs))
		_ = groups // suppress unused warning when verbose is false
	}
}

// jsonReport is the JSON output structure.
type jsonReport struct {
	DeadFiles   []jsonDeadFile `json:"dead_files"`
	DeadSymbols []jsonSymbol   `json:"dead_symbols"`
	Summary     jsonSummary    `json:"summary"`
}

type jsonDeadFile struct {
	File    string       `json:"file"`
	Package string       `json:"package"`
	Symbols []jsonSymbol `json:"symbols"`
}

type jsonSymbol struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Package  string `json:"package"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Exported bool   `json:"exported"`
	Receiver string `json:"receiver,omitempty"`
}

type jsonSummary struct {
	DeadFileCount int `json:"dead_file_count"`
	DeadCount     int `json:"dead_count"`
	TotalDefs     int `json:"total_defs"`
	TotalRefs     int `json:"total_refs"`
	PackageCount  int `json:"package_count"`
}

// ReportJSON outputs dead code report in JSON format.
func ReportJSON(w io.Writer, result *AnalysisResult) {
	groups := groupByPackage(result.DeadSymbols)

	// Convert dead files.
	deadFiles := make([]jsonDeadFile, len(result.DeadFiles))
	for i, df := range result.DeadFiles {
		syms := make([]jsonSymbol, len(df.Symbols))
		for j, sym := range df.Symbols {
			syms[j] = jsonSymbol{
				Name:     sym.Name,
				Kind:     string(sym.Kind),
				Package:  sym.Package,
				File:     sym.File,
				Line:     sym.Line,
				Exported: sym.Exported,
				Receiver: sym.Receiver,
			}
		}
		deadFiles[i] = jsonDeadFile{
			File:    df.File,
			Package: df.Package,
			Symbols: syms,
		}
	}

	// Convert dead symbols.
	symbols := make([]jsonSymbol, len(result.DeadSymbols))
	for i, def := range result.DeadSymbols {
		symbols[i] = jsonSymbol{
			Name:     def.Name,
			Kind:     string(def.Kind),
			Package:  def.Package,
			File:     def.File,
			Line:     def.Line,
			Exported: def.Exported,
			Receiver: def.Receiver,
		}
	}

	// Count total dead symbols including dead file members.
	totalDead := len(result.DeadSymbols)
	for _, df := range result.DeadFiles {
		totalDead += len(df.Symbols)
	}

	report := jsonReport{
		DeadFiles:   deadFiles,
		DeadSymbols: symbols,
		Summary: jsonSummary{
			DeadFileCount: len(result.DeadFiles),
			DeadCount:     totalDead,
			TotalDefs:     len(result.AllDefs),
			TotalRefs:     len(result.AllRefs),
			PackageCount:  len(groups),
		},
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(report) //nolint:errcheck
}

// groupByPackage groups symbol definitions by package name.
func groupByPackage(defs []SymbolDef) map[string][]SymbolDef {
	groups := make(map[string][]SymbolDef)
	for _, def := range defs {
		groups[def.Package] = append(groups[def.Package], def)
	}
	return groups
}
