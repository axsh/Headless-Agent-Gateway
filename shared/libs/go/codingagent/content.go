package codingagent

import (
	"errors"
	"fmt"
	"strings"
)

// ErrMultimodalNotSupported is returned by agents that do not support
// non-text content (e.g., images).
var ErrMultimodalNotSupported = errors.New("multimodal inputs are not supported by this agent")

// ContentPart represents a single block in a multimodal message.
// Supported types: "text" (plain text) and "image" (base64-encoded image).
type ContentPart struct {
	Type   string       `json:"type"`             // "text" or "image"
	Text   string       `json:"text,omitempty"`   // populated when type="text"
	Source *ImageSource `json:"source,omitempty"` // populated when type="image"
}

// ImageSource holds image data for a ContentPart of type "image".
type ImageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // MIME type (e.g., "image/png", "image/jpeg")
	Data      string `json:"data"`       // Base64-encoded image data
}

// ExtractText concatenates all text blocks from a []ContentPart.
// Non-text blocks are ignored. Returns empty string if no text blocks exist.
func ExtractText(parts []ContentPart) string {
	var sb strings.Builder
	for _, p := range parts {
		if p.Type == "text" && p.Text != "" {
			sb.WriteString(p.Text)
		}
	}
	return sb.String()
}

// HasNonTextContent returns true if any ContentPart has a type other than "text".
func HasNonTextContent(parts []ContentPart) bool {
	for _, p := range parts {
		if p.Type != "text" {
			return true
		}
	}
	return false
}

// TextOnlyContent creates a []ContentPart from a plain text string.
// Used by v1 handler to wrap legacy "message" field into content blocks.
func TextOnlyContent(text string) []ContentPart {
	return []ContentPart{{Type: "text", Text: text}}
}

// ValidateContentParts validates a []ContentPart for correctness.
// Returns an error if any part is invalid.
func ValidateContentParts(parts []ContentPart) error {
	if len(parts) == 0 {
		return fmt.Errorf("content must not be empty")
	}
	for i, p := range parts {
		if p.Type == "" {
			return fmt.Errorf("content[%d]: content type must not be empty", i)
		}
		switch p.Type {
		case "text":
			// text blocks are always valid (empty text is allowed)
		case "image":
			if p.Source == nil {
				return fmt.Errorf("content[%d]: image content part requires source", i)
			}
			if p.Source.MediaType == "" {
				return fmt.Errorf("content[%d]: image source media_type must not be empty", i)
			}
			if p.Source.Data == "" {
				return fmt.Errorf("content[%d]: image source data must not be empty", i)
			}
		default:
			return fmt.Errorf("content[%d]: unsupported content type: %q", i, p.Type)
		}
	}
	return nil
}
