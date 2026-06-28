package agentservice

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

// SaveImageToTempFile decodes Base64 image data and saves it to
// {baseDir}/tmp/multimodal/{sessionID}_{hash}.{ext}.
// Returns the absolute path to the saved file.
func SaveImageToTempFile(baseDir string, sessionID string, source *codingagent.ImageSource) (string, error) {
	if source.Data == "" {
		return "", fmt.Errorf("image data is empty")
	}
	decoded, err := base64.StdEncoding.DecodeString(source.Data)
	if err != nil {
		return "", fmt.Errorf("invalid base64 data: %w", err)
	}

	ext := mediaTypeToExt(source.MediaType)
	hash := sha256.Sum256(decoded)
	filename := fmt.Sprintf("%s_%s%s", sessionID, hex.EncodeToString(hash[:8]), ext)

	dir := filepath.Join(baseDir, "tmp", "multimodal")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create multimodal dir: %w", err)
	}

	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, decoded, 0644); err != nil {
		return "", fmt.Errorf("write image file: %w", err)
	}
	return path, nil
}

// BuildMultimodalPrompt processes []ContentPart:
// - text blocks are concatenated
// - image blocks are saved to temp files and replaced with path references
// Returns the combined prompt string and a list of saved file paths for cleanup.
func BuildMultimodalPrompt(baseDir, sessionID string, parts []codingagent.ContentPart) (string, []string, error) {
	var sb strings.Builder
	var savedFiles []string
	for _, p := range parts {
		switch p.Type {
		case "text":
			sb.WriteString(p.Text)
		case "image":
			if p.Source == nil {
				return "", nil, fmt.Errorf("image content part missing source")
			}
			path, err := SaveImageToTempFile(baseDir, sessionID, p.Source)
			if err != nil {
				return "", nil, fmt.Errorf("save image: %w", err)
			}
			savedFiles = append(savedFiles, path)
			sb.WriteString(fmt.Sprintf("\n[Attached image: %s]\n", path))
		}
	}
	return sb.String(), savedFiles, nil
}

// CleanupMultimodalFiles removes all temp files created for a session.
func CleanupMultimodalFiles(paths []string) {
	for _, p := range paths {
		os.Remove(p)
	}
}

// mediaTypeToExt maps a MIME media type to a file extension.
func mediaTypeToExt(mediaType string) string {
	switch strings.ToLower(mediaType) {
	case "image/png":
		return ".png"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".bin"
	}
}
