package analyzer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// AnalysisResult holds the complete analysis results.
type AnalysisResult struct {
	DeadSymbols []SymbolDef // symbols with no references
	AllDefs     []SymbolDef // all collected definitions
	AllRefs     []SymbolRef // all collected references
}

// Analyze scans the directory tree and finds dead code.
func Analyze(rootDir string, excludePatterns []string) (*AnalysisResult, error) {
	var allDefs []SymbolDef
	var allRefs []SymbolRef

	// Walk the directory tree and collect symbols from all Go files.
	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip excluded directories.
		if info.IsDir() {
			base := info.Name()
			for _, pattern := range excludePatterns {
				pattern = strings.TrimSpace(pattern)
				if pattern != "" && matchesExclude(path, base, pattern) {
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Only process .go files.
		if !strings.HasSuffix(info.Name(), ".go") {
			return nil
		}

		source, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}

		tree, err := ParseFile(source)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		defer tree.Close()

		// Extract package name from AST.
		pkgName := extractPackageName(tree, source)
		isTestFile := strings.HasSuffix(info.Name(), "_test.go")

		defs, refs := CollectSymbols(tree, source, path, pkgName, isTestFile)
		allDefs = append(allDefs, defs...)
		allRefs = append(allRefs, refs...)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", rootDir, err)
	}

	// Build reference index: map[name] -> list of refs.
	refIndex := buildRefIndex(allRefs)

	// Determine dead symbols.
	var deadSymbols []SymbolDef
	for _, def := range allDefs {
		if shouldSkip(def) {
			continue
		}
		if def.Ignored {
			continue
		}
		if !hasReferences(def, refIndex) {
			deadSymbols = append(deadSymbols, def)
		}
	}

	return &AnalysisResult{
		DeadSymbols: deadSymbols,
		AllDefs:     allDefs,
		AllRefs:     allRefs,
	}, nil
}

// extractPackageName gets the package name from the AST.
func extractPackageName(tree *sitter.Tree, source []byte) string {
	root := tree.RootNode()
	for i := range root.ChildCount() {
		child := root.Child(i)
		if child != nil && child.Kind() == "package_clause" {
			// package_clause has a child which is the package name identifier.
			for j := range child.ChildCount() {
				nameNode := child.Child(j)
				if nameNode != nil && nameNode.Kind() == "package_identifier" {
					return nodeText(nameNode, source)
				}
			}
		}
	}
	return ""
}

// buildRefIndex creates a map from symbol name to list of references.
func buildRefIndex(refs []SymbolRef) map[string][]SymbolRef {
	index := make(map[string][]SymbolRef)
	for _, ref := range refs {
		index[ref.Name] = append(index[ref.Name], ref)
	}
	return index
}

// shouldSkip returns true if the symbol should be excluded from dead code analysis.
func shouldSkip(def SymbolDef) bool {
	// Skip init() and main() functions.
	if def.Kind == KindFunc && (def.Name == "init" || def.Name == "main") {
		return true
	}
	// Skip the blank identifier.
	if def.Name == "_" {
		return true
	}
	return false
}

// hasReferences checks if a symbol has references outside its own definition.
func hasReferences(def SymbolDef, refIndex map[string][]SymbolRef) bool {
	refs, ok := refIndex[def.Name]
	if !ok {
		return false
	}

	for _, ref := range refs {
		// Skip self-references (same file, same line as definition).
		if ref.File == def.File && ref.Line == def.Line {
			continue
		}

		if def.Exported {
			// Exported symbols: any reference from anywhere counts.
			return true
		}
		// Unexported symbols: only references from the same package count.
		if ref.Package == def.Package {
			return true
		}
	}
	return false
}

// matchesExclude checks if a path or directory name matches an exclude pattern.
func matchesExclude(path, baseName, pattern string) bool {
	// Simple matching: check if the base name equals the pattern,
	// or if the pattern appears in the path.
	if baseName == pattern {
		return true
	}
	return strings.Contains(path, pattern)
}
