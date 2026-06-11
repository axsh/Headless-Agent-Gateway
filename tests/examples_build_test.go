//go:build integration

package llm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// nullOutput returns the platform-appropriate null device path.
func nullOutput() string {
	if runtime.GOOS == "windows" {
		return "NUL"
	}
	return "/dev/null"
}

// TestExamples_MinimalServer_Builds verifies that the minimal-server example
// compiles without errors. This ensures the public API surface used by the
// example remains compatible.
func TestExamples_MinimalServer_Builds(t *testing.T) {
	projectRoot, _ := filepath.Abs("..")
	exampleDir := filepath.Join(projectRoot, "examples", "minimal-server")

	if _, err := os.Stat(exampleDir); os.IsNotExist(err) {
		t.Fatalf("example directory does not exist: %s", exampleDir)
	}

	cmd := exec.Command("go", "build", "-o", nullOutput(), ".")
	cmd.Dir = exampleDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("minimal-server build failed: %v\n%s", err, output)
	}
}

// TestExamples_MinimalClient_Builds verifies that the minimal-client example
// compiles without errors.
func TestExamples_MinimalClient_Builds(t *testing.T) {
	projectRoot, _ := filepath.Abs("..")
	exampleDir := filepath.Join(projectRoot, "examples", "minimal-client")

	if _, err := os.Stat(exampleDir); os.IsNotExist(err) {
		t.Fatalf("example directory does not exist: %s", exampleDir)
	}

	cmd := exec.Command("go", "build", "-o", nullOutput(), ".")
	cmd.Dir = exampleDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("minimal-client build failed: %v\n%s", err, output)
	}
}

// TestExamples_CawaServer_Builds verifies that the cawa-server example
// compiles without errors.
func TestExamples_CawaServer_Builds(t *testing.T) {
	projectRoot, _ := filepath.Abs("..")
	exampleDir := filepath.Join(projectRoot, "examples", "cawa-server")

	if _, err := os.Stat(exampleDir); os.IsNotExist(err) {
		t.Fatalf("example directory does not exist: %s", exampleDir)
	}

	cmd := exec.Command("go", "build", "-o", nullOutput(), ".")
	cmd.Dir = exampleDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cawa-server build failed: %v\n%s", err, output)
	}
}
