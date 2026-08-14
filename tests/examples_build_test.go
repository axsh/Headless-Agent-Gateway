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

// TestFeatures_Tern_Builds verifies that the tern feature
// compiles without errors.
func TestFeatures_Tern_Builds(t *testing.T) {
	projectRoot, _ := filepath.Abs("..")
	featureDir := filepath.Join(projectRoot, "features", "tern")

	if _, err := os.Stat(featureDir); os.IsNotExist(err) {
		t.Fatalf("feature directory does not exist: %s", featureDir)
	}

	cmd := exec.Command("go", "build", "-o", nullOutput(), ".")
	cmd.Dir = featureDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tern build failed: %v\n%s", err, output)
	}
}

// TestFeatures_Ternctl_Builds verifies that the ternctl feature
// compiles without errors.
func TestFeatures_Ternctl_Builds(t *testing.T) {
	projectRoot, _ := filepath.Abs("..")
	featureDir := filepath.Join(projectRoot, "features", "ternctl")

	if _, err := os.Stat(featureDir); os.IsNotExist(err) {
		t.Fatalf("feature directory does not exist: %s", featureDir)
	}

	cmd := exec.Command("go", "build", "-o", nullOutput(), ".")
	cmd.Dir = featureDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ternctl build failed: %v\n%s", err, output)
	}
}

// TestExamples_MultimodalClient_Builds verifies that the multimodal-client example
// compiles without errors.
func TestExamples_MultimodalClient_Builds(t *testing.T) {
	projectRoot, _ := filepath.Abs("..")
	exampleDir := filepath.Join(projectRoot, "examples", "multimodal-client")

	if _, err := os.Stat(exampleDir); os.IsNotExist(err) {
		t.Fatalf("example directory does not exist: %s", exampleDir)
	}

	cmd := exec.Command("go", "build", "-o", nullOutput(), ".")
	cmd.Dir = exampleDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("multimodal-client build failed: %v\n%s", err, output)
	}
}

// TestExamples_ArtifactPipeline_Builds verifies that the artifact-pipeline example
// compiles without errors.
func TestExamples_ArtifactPipeline_Builds(t *testing.T) {
	projectRoot, _ := filepath.Abs("..")
	exampleDir := filepath.Join(projectRoot, "examples", "artifact-pipeline")

	if _, err := os.Stat(exampleDir); os.IsNotExist(err) {
		t.Fatalf("example directory does not exist: %s", exampleDir)
	}

	cmd := exec.Command("go", "build", "-o", nullOutput(), ".")
	cmd.Dir = exampleDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("artifact-pipeline build failed: %v\n%s", err, output)
	}
}

// TestExamples_EmbeddingsClient_Builds verifies that the embeddings-client example
// compiles without errors.
func TestExamples_EmbeddingsClient_Builds(t *testing.T) {
	projectRoot, _ := filepath.Abs("..")
	exampleDir := filepath.Join(projectRoot, "examples", "embeddings-client")

	if _, err := os.Stat(exampleDir); os.IsNotExist(err) {
		t.Fatalf("example directory does not exist: %s", exampleDir)
	}

	cmd := exec.Command("go", "build", "-o", nullOutput(), ".")
	cmd.Dir = exampleDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("embeddings-client build failed: %v\n%s", err, output)
	}
}
