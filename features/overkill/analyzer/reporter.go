package analyzer

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// ReportText outputs dead code report in human-readable format.
func ReportText(w io.Writer, result *AnalysisResult, verbose bool) {
	if len(result.DeadSymbols) == 0 {
		fmt.Fprintln(w, "No dead code found.")
		return
	}

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

	// Summary.
	fmt.Fprintf(w, "Summary: %d dead symbols found across %d packages\n",
		len(result.DeadSymbols), len(groups))

	if verbose {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Total definitions: %d\n", len(result.AllDefs))
		fmt.Fprintf(w, "Total references:  %d\n", len(result.AllRefs))
	}
}

// jsonReport is the JSON output structure.
type jsonReport struct {
	DeadSymbols []jsonSymbol `json:"dead_symbols"`
	Summary     jsonSummary  `json:"summary"`
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
	DeadCount    int `json:"dead_count"`
	TotalDefs    int `json:"total_defs"`
	TotalRefs    int `json:"total_refs"`
	PackageCount int `json:"package_count"`
}

// ReportJSON outputs dead code report in JSON format.
func ReportJSON(w io.Writer, result *AnalysisResult) {
	groups := groupByPackage(result.DeadSymbols)

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

	report := jsonReport{
		DeadSymbols: symbols,
		Summary: jsonSummary{
			DeadCount:    len(result.DeadSymbols),
			TotalDefs:    len(result.AllDefs),
			TotalRefs:    len(result.AllRefs),
			PackageCount: len(groups),
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
