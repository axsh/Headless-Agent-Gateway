package llm_test

import (
	"path/filepath"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
	"github.com/stretchr/testify/require"
)

// AssertSystemArtifactPathsContain fails unless every want path appears in listed
// artifacts (match on Key basename or full Key).
func AssertSystemArtifactPathsContain(t *testing.T, listed []store.SystemArtifactEvent, wantPaths ...string) {
	t.Helper()
	have := make(map[string]bool, len(listed)*2)
	for _, item := range listed {
		have[item.Key] = true
		have[filepath.Base(item.Key)] = true
		have[filepath.ToSlash(item.Key)] = true
	}
	for _, want := range wantPaths {
		if have[want] || have[filepath.Base(want)] || have[filepath.ToSlash(want)] {
			continue
		}
		require.Failf(t, "missing system artifact path", "want %q in %+v", want, listed)
	}
}
