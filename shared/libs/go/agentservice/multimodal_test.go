package agentservice

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testImageData returns a small valid PNG header encoded as base64
// (not a real image, but sufficient for testing file I/O).
func testImageData() string {
	data := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	return base64.StdEncoding.EncodeToString(data)
}

func TestSaveImageToTempFile_PNG(t *testing.T) {
	source := &codingagent.ImageSource{
		Type:      "base64",
		MediaType: "image/png",
		Data:      testImageData(),
	}

	path, err := SaveImageToTempFile("session-001", source)
	require.NoError(t, err)
	require.NotEmpty(t, path)
	t.Cleanup(func() { os.Remove(path) })

	// Verify file exists.
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.False(t, info.IsDir())

	// Verify file is under {TempDir}/arctic-tern-multimodal/.
	expectedDir := filepath.Join(os.TempDir(), "arctic-tern-multimodal")
	assert.True(t, strings.HasPrefix(path, expectedDir))

	// Verify extension.
	assert.True(t, strings.HasSuffix(path, ".png"))

	// Verify file content matches decoded base64.
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	expected, _ := base64.StdEncoding.DecodeString(testImageData())
	assert.Equal(t, expected, content)
}

func TestSaveImageToTempFile_JPEG(t *testing.T) {
	source := &codingagent.ImageSource{
		Type:      "base64",
		MediaType: "image/jpeg",
		Data:      testImageData(),
	}

	path, err := SaveImageToTempFile("session-002", source)
	require.NoError(t, err)
	t.Cleanup(func() { os.Remove(path) })
	assert.True(t, strings.HasSuffix(path, ".jpg"))
}

func TestSaveImageToTempFile_InvalidBase64(t *testing.T) {
	source := &codingagent.ImageSource{
		Type:      "base64",
		MediaType: "image/png",
		Data:      "!!!not-valid-base64!!!",
	}

	_, err := SaveImageToTempFile("session-003", source)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid base64 data")
}

func TestSaveImageToTempFile_EmptyData(t *testing.T) {
	source := &codingagent.ImageSource{
		Type:      "base64",
		MediaType: "image/png",
		Data:      "",
	}

	_, err := SaveImageToTempFile("session-004", source)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "image data is empty")
}

func TestBuildMultimodalPrompt(t *testing.T) {
	parts := []codingagent.ContentPart{
		{Type: "text", Text: "Look at this image: "},
		{Type: "image", Source: &codingagent.ImageSource{
			Type: "base64", MediaType: "image/png", Data: testImageData(),
		}},
		{Type: "text", Text: "What do you see?"},
	}

	prompt, savedFiles, err := BuildMultimodalPrompt("session-005", parts)
	require.NoError(t, err)
	t.Cleanup(func() { CleanupMultimodalFiles(savedFiles) })

	// Text parts are concatenated.
	assert.Contains(t, prompt, "Look at this image: ")
	assert.Contains(t, prompt, "What do you see?")

	// Image reference is embedded.
	assert.Contains(t, prompt, "[Attached image:")
	assert.Len(t, savedFiles, 1)

	// Saved file exists.
	_, err = os.Stat(savedFiles[0])
	require.NoError(t, err)
}

func TestBuildMultimodalPrompt_TextOnly(t *testing.T) {
	parts := []codingagent.ContentPart{
		{Type: "text", Text: "Hello, world!"},
	}

	prompt, savedFiles, err := BuildMultimodalPrompt("session-006", parts)
	require.NoError(t, err)
	assert.Equal(t, "Hello, world!", prompt)
	assert.Empty(t, savedFiles)
}

func TestBuildMultimodalPrompt_ImageMissingSource(t *testing.T) {
	parts := []codingagent.ContentPart{
		{Type: "image"}, // Source is nil
	}

	_, _, err := BuildMultimodalPrompt("session-007", parts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "image content part missing source")
}

func TestCleanupMultimodalFiles(t *testing.T) {
	source := &codingagent.ImageSource{
		Type: "base64", MediaType: "image/png", Data: testImageData(),
	}

	// Save two files.
	path1, err := SaveImageToTempFile("session-008a", source)
	require.NoError(t, err)
	path2, err := SaveImageToTempFile("session-008b", source)
	require.NoError(t, err)

	// Verify files exist before cleanup.
	_, err = os.Stat(path1)
	require.NoError(t, err)
	_, err = os.Stat(path2)
	require.NoError(t, err)

	// Cleanup.
	CleanupMultimodalFiles([]string{path1, path2})

	// Verify files no longer exist.
	_, err = os.Stat(path1)
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(path2)
	assert.True(t, os.IsNotExist(err))
}

func TestCleanupMultimodalFiles_EmptyList(t *testing.T) {
	// Should not panic with empty list.
	CleanupMultimodalFiles(nil)
	CleanupMultimodalFiles([]string{})
}

func TestMediaTypeToExt(t *testing.T) {
	tests := []struct {
		mediaType string
		expected  string
	}{
		{"image/png", ".png"},
		{"image/jpeg", ".jpg"},
		{"image/jpg", ".jpg"},
		{"image/gif", ".gif"},
		{"image/webp", ".webp"},
		{"IMAGE/PNG", ".png"},
		{"application/octet-stream", ".bin"},
		{"", ".bin"},
	}

	for _, tt := range tests {
		t.Run(tt.mediaType, func(t *testing.T) {
			result := mediaTypeToExt(tt.mediaType)
			assert.Equal(t, tt.expected, result)
		})
	}
}
