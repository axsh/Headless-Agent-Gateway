package llmgateway

import (
	"os"
	"testing"
)

// TestMain registers test providers before running tests.
// This replaces the init()-based registration that happens in subpackages.
func TestMain(m *testing.M) {
	registerTestProviders()
	os.Exit(m.Run())
}
