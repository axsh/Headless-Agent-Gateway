package v1

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// ContentPart represents a single content block in a v1 message.
type ContentPart struct {
	Type   string       `json:"type"`
	Text   string       `json:"text,omitempty"`
	Source *ImageSource `json:"source,omitempty"`
}

// ImageSource contains base64-encoded image data.
type ImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

// Message builds a list of ContentPart for multimodal messages.
type Message struct {
	parts []ContentPart
	err   error
}

// NewMessage creates a new Message builder.
func NewMessage() *Message {
	return &Message{}
}

// Text adds a text content part to the message.
func (m *Message) Text(text string) *Message {
	if m.err != nil {
		return m
	}
	m.parts = append(m.parts, ContentPart{
		Type: "text",
		Text: text,
	})
	return m
}

// ImageBase64 adds an image content part from a base64 encoded string.
func (m *Message) ImageBase64(mediaType string, base64Data string) *Message {
	if m.err != nil {
		return m
	}
	m.parts = append(m.parts, ContentPart{
		Type: "image",
		Source: &ImageSource{
			Type:      "base64",
			MediaType: mediaType,
			Data:      base64Data,
		},
	})
	return m
}

// ImageBytes adds an image content part from raw binary data, detecting media type automatically.
func (m *Message) ImageBytes(data []byte) *Message {
	if m.err != nil {
		return m
	}
	mediaType := http.DetectContentType(data)
	if !strings.HasPrefix(mediaType, "image/") {
		m.err = fmt.Errorf("inferred content type %q is not a supported image type", mediaType)
		return m
	}
	base64Data := base64.StdEncoding.EncodeToString(data)
	return m.ImageBase64(mediaType, base64Data)
}

// ImageFile reads an image file from path and adds it as an image content part.
// The media type is automatically detected from the file content.
func (m *Message) ImageFile(path string) *Message {
	if m.err != nil {
		return m
	}
	data, err := os.ReadFile(path)
	if err != nil {
		m.err = fmt.Errorf("read image file: %w", err)
		return m
	}
	return m.ImageBytes(data)
}

// ImageReader reads image data from r and adds it as an image content part.
// The media type is automatically detected from the content.
func (m *Message) ImageReader(r io.Reader) *Message {
	if m.err != nil {
		return m
	}
	data, err := io.ReadAll(r)
	if err != nil {
		m.err = fmt.Errorf("read image data: %w", err)
		return m
	}
	return m.ImageBytes(data)
}

// Build returns the compiled list of ContentPart, or an error if any operation failed.
func (m *Message) Build() ([]ContentPart, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.parts, nil
}
