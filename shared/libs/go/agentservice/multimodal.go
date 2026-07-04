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
	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/session"
)

// SaveImageToTempFile decodes Base64 image data and saves it to
// {baseDir}/tmp/multimodal/{sessionID}_{hash}.{ext}.
// Returns the absolute path to the saved file.
func SaveImageToTempFile(baseDir string, sessionID string, source *codingagent.ImageSource) (string, error) {
	return saveImage(filepath.Join(baseDir, "tmp", "multimodal"), sessionID+"_", source)
}

func saveImage(dir string, prefix string, source *codingagent.ImageSource) (string, error) {
	if source.Data == "" {
		return "", fmt.Errorf("image data is empty")
	}
	decoded, err := base64.StdEncoding.DecodeString(source.Data)
	if err != nil {
		return "", fmt.Errorf("invalid base64 data: %w", err)
	}

	ext := mediaTypeToExt(source.MediaType)
	hash := sha256.Sum256(decoded)
	filename := fmt.Sprintf("%s%s%s", prefix, hex.EncodeToString(hash[:8]), ext)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create multimodal dir: %w", err)
	}

	path := filepath.Join(dir, filename)
	// Skip if file already exists.
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

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

// AppendSessionMessage appends a message to the session history,
// automatically determining the next sequence number.
func AppendSessionMessage(sessionDir string, msg session.Message) error {
	histDir := filepath.Join(sessionDir, "history")
	if err := os.MkdirAll(histDir, 0755); err != nil {
		return fmt.Errorf("create history dir: %w", err)
	}

	// Find the next sequence number by looking at existing files.
	files, err := os.ReadDir(histDir)
	if err != nil {
		return fmt.Errorf("read history dir: %w", err)
	}

	maxSeq := 0
	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".json") {
			var seq int
			if _, err := fmt.Sscanf(f.Name(), "%x.json", &seq); err == nil {
				if seq > maxSeq {
					maxSeq = seq
				}
			}
		}
	}

	msg.Seq = maxSeq + 1
	return session.AppendHistory(histDir, []session.Message{msg})
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
