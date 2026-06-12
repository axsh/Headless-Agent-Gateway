package analyzer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// DeadFile represents a file where all definitions are dead.
type DeadFile struct {
	File    string      // file path
	Package string      // Go package name
	Symbols []SymbolDef // dead symbols in this file
}

// AnalysisResult holds the complete analysis results.
type AnalysisResult struct {
	DeadSymbols []SymbolDef // symbols with no references (excluding dead file members)
	DeadFiles   []DeadFile  // files where all definitions are dead
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

	// Detect dead files: files where ALL definitions are dead.
	deadFiles := detectDeadFiles(deadSymbols, allDefs)

	// Remove symbols belonging to dead files from DeadSymbols
	// to avoid duplicate reporting.
	if len(deadFiles) > 0 {
		deadFileSymbols := make(map[string]bool)
		for _, df := range deadFiles {
			for _, sym := range df.Symbols {
				key := df.File + ":" + sym.Name
				deadFileSymbols[key] = true
			}
		}
		var filtered []SymbolDef
		for _, sym := range deadSymbols {
			key := sym.File + ":" + sym.Name
			if !deadFileSymbols[key] {
				filtered = append(filtered, sym)
			}
		}
		deadSymbols = filtered
	}

	return &AnalysisResult{
		DeadSymbols: deadSymbols,
		DeadFiles:   deadFiles,
		AllDefs:     allDefs,
		AllRefs:     allRefs,
	}, nil
}

// detectDeadFiles identifies files where all non-skippable definitions are dead.
func detectDeadFiles(deadSymbols []SymbolDef, allDefs []SymbolDef) []DeadFile {
	// Build dead symbol index: file+name -> bool.
	deadSet := make(map[string]bool)
	for _, sym := range deadSymbols {
		key := sym.File + ":" + sym.Name
		deadSet[key] = true
	}

	// Group all definitions by file.
	fileDefs := make(map[string][]SymbolDef)
	for _, def := range allDefs {
		fileDefs[def.File] = append(fileDefs[def.File], def)
	}

	var deadFiles []DeadFile
	for filePath, defs := range fileDefs {
		// Count non-skippable definitions.
		var checkable []SymbolDef
		for _, def := range defs {
			if shouldSkip(def) || def.Ignored {
				continue
			}
			checkable = append(checkable, def)
		}

		// Skip files with no checkable definitions.
		if len(checkable) == 0 {
			continue
		}

		// Check if ALL checkable definitions are dead.
		allDead := true
		for _, def := range checkable {
			key := def.File + ":" + def.Name
			if !deadSet[key] {
				allDead = false
				break
			}
		}

		if allDead {
			pkgName := ""
			if len(defs) > 0 {
				pkgName = defs[0].Package
			}
			deadFiles = append(deadFiles, DeadFile{
				File:    filePath,
				Package: pkgName,
				Symbols: checkable,
			})
		}
	}

	return deadFiles
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
