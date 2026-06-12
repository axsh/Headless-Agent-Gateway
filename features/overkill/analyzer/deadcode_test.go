package analyzer

import (
	"os"
	"path/filepath"
	"testing"
)

// setupTestDir creates a temporary directory with Go source files for testing.
func setupTestDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return dir
}

func TestAnalyze_DetectsUnusedExportedFunc(t *testing.T) {
	dir := setupTestDir(t, map[string]string{
		"main.go": `package main

func main() {
	Used()
}

func Used() {}

func Unused() {}
`,
	})

	result, err := Analyze(dir, nil)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if len(result.DeadSymbols) != 1 {
		t.Fatalf("expected 1 dead symbol, got %d: %v", len(result.DeadSymbols), symbolNames(result.DeadSymbols))
	}
	if result.DeadSymbols[0].Name != "Unused" {
		t.Errorf("expected dead symbol 'Unused', got %q", result.DeadSymbols[0].Name)
	}
}

func TestAnalyze_SkipsInit(t *testing.T) {
	dir := setupTestDir(t, map[string]string{
		"lib.go": `package lib

func init() {
	setup()
}

func setup() {}
`,
	})

	result, err := Analyze(dir, nil)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// init() should not be reported as dead.
	for _, dead := range result.DeadSymbols {
		if dead.Name == "init" {
			t.Error("init() should not be reported as dead code")
		}
	}
}

func TestAnalyze_SkipsMain(t *testing.T) {
	dir := setupTestDir(t, map[string]string{
		"main.go": `package main

func main() {}
`,
	})

	result, err := Analyze(dir, nil)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	for _, dead := range result.DeadSymbols {
		if dead.Name == "main" {
			t.Error("main() should not be reported as dead code")
		}
	}
}

func TestAnalyze_SkipsUsedFunc(t *testing.T) {
	dir := setupTestDir(t, map[string]string{
		"lib.go": `package lib

func Public() string {
	return helper()
}

func helper() string {
	return "hi"
}
`,
	})

	result, err := Analyze(dir, nil)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// helper() is used by Public(), so it should not be dead.
	for _, dead := range result.DeadSymbols {
		if dead.Name == "helper" {
			t.Error("helper() is used by Public() and should not be dead")
		}
	}
}

func TestAnalyze_CrossPackageReference(t *testing.T) {
	dir := setupTestDir(t, map[string]string{
		"pkg/lib.go": `package pkg

func Exported() string {
	return "hello"
}
`,
		"main.go": `package main

import "pkg"

func main() {
	pkg.Exported()
}
`,
	})

	result, err := Analyze(dir, nil)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// Exported() is referenced in main.go, but as "Exported" identifier.
	// Our simple analysis uses name-based matching, so it should be found.
	for _, dead := range result.DeadSymbols {
		if dead.Name == "Exported" {
			t.Error("Exported() is referenced cross-package and should not be dead")
		}
	}
}

func TestAnalyze_TestFileReference(t *testing.T) {
	dir := setupTestDir(t, map[string]string{
		"lib.go": `package lib

func OnlyUsedInTest() string {
	return "test"
}
`,
		"lib_test.go": `package lib

import "testing"

func TestIt(t *testing.T) {
	OnlyUsedInTest()
}
`,
	})

	result, err := Analyze(dir, nil)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// OnlyUsedInTest is referenced from a test file, so it should not be dead.
	for _, dead := range result.DeadSymbols {
		if dead.Name == "OnlyUsedInTest" {
			t.Error("OnlyUsedInTest() is referenced from test file and should not be dead")
		}
	}
}

func TestAnalyze_IgnoredSymbol(t *testing.T) {
	dir := setupTestDir(t, map[string]string{
		"lib.go": `package lib

// overkill:ignore
func Ignored() {}

func NotIgnored() {}
`,
	})

	result, err := Analyze(dir, nil)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// Ignored() has the overkill:ignore comment and should not be reported.
	for _, dead := range result.DeadSymbols {
		if dead.Name == "Ignored" {
			t.Error("Ignored() has overkill:ignore and should not be dead")
		}
	}

	// NotIgnored() has no references and should be dead.
	found := false
	for _, dead := range result.DeadSymbols {
		if dead.Name == "NotIgnored" {
			found = true
		}
	}
	if !found {
		t.Error("NotIgnored() should be reported as dead")
	}
}

// symbolNames is a test helper that returns symbol names as a slice.
func symbolNames(defs []SymbolDef) []string {
	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.Name
	}
	return names
}

func TestAnalyze_DeadFile(t *testing.T) {
	dir := setupTestDir(t, map[string]string{
		"main.go": `package main

func main() { Used() }

func Used() {}
`,
		"dead.go": `package main

func DeadA() {}

func DeadB() {}
`,
	})

	result, err := Analyze(dir, nil)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// dead.go should be detected as a dead file.
	if len(result.DeadFiles) != 1 {
		t.Fatalf("expected 1 dead file, got %d", len(result.DeadFiles))
	}
	if result.DeadFiles[0].File != filepath.Join(dir, "dead.go") {
		t.Errorf("expected dead file 'dead.go', got %q", result.DeadFiles[0].File)
	}

	// DeadA and DeadB should NOT be in DeadSymbols (duplicate report exclusion).
	for _, sym := range result.DeadSymbols {
		if sym.Name == "DeadA" || sym.Name == "DeadB" {
			t.Errorf("dead file symbol %q should not be in DeadSymbols", sym.Name)
		}
	}
}

func TestAnalyze_PartiallyDeadFile(t *testing.T) {
	dir := setupTestDir(t, map[string]string{
		"lib.go": `package lib

func Used() string { return helper() }

func helper() string { return "hi" }

func Unused() {}
`,
	})

	result, err := Analyze(dir, nil)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// lib.go has both used and unused symbols, should NOT be a dead file.
	if len(result.DeadFiles) != 0 {
		t.Errorf("expected 0 dead files for partially dead file, got %d", len(result.DeadFiles))
	}

	// Unused should still be in DeadSymbols.
	found := false
	for _, sym := range result.DeadSymbols {
		if sym.Name == "Unused" {
			found = true
		}
	}
	if !found {
		t.Error("Unused should be in DeadSymbols")
	}
}

func TestExecute_DeadFile(t *testing.T) {
	dir := setupTestDir(t, map[string]string{
		"main.go": `package main

func main() { Used() }

func Used() {}
`,
		"dead.go": `package main

func DeadA() {}

func DeadB() {}
`,
	})

	result, err := Analyze(dir, nil)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	removed, err := Execute(result)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if removed != 2 {
		t.Errorf("expected 2 removed (1 file with 2 symbols), got %d", removed)
	}

	// dead.go should no longer exist.
	if _, err := os.Stat(filepath.Join(dir, "dead.go")); !os.IsNotExist(err) {
		t.Error("dead.go should have been deleted")
	}

	// main.go should still exist.
	if _, err := os.Stat(filepath.Join(dir, "main.go")); err != nil {
		t.Errorf("main.go should still exist: %v", err)
	}
}

