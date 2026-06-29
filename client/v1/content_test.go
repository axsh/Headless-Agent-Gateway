package v1_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	client "github.com/axsh/arctic-tern/client/v1"
)

// 1x1 red transparent PNG image (valid image data)
var validPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
	0x0d, 0x49, 0x44, 0x41, 0x54, 0x78, 0x01, 0x63, 0xfc, 0xcf, 0xc0, 0x50,
	0x0f, 0x00, 0x04, 0x85, 0x01, 0x80, 0x84, 0xa9, 0x8c, 0x21, 0x00, 0x00,
	0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

func TestMessageBuilder_Success(t *testing.T) {
	parts, err := client.NewMessage().
		Text("hello").
		ImageBase64("image/png", "base64data").
		ImageBytes(validPNG).
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(parts) != 3 {
		t.Errorf("expected 3 parts, got %d", len(parts))
	}

	if parts[0].Type != "text" || parts[0].Text != "hello" {
		t.Errorf("invalid text part: %+v", parts[0])
	}

	if parts[1].Type != "image" || parts[1].Source.MediaType != "image/png" || parts[1].Source.Data != "base64data" {
		t.Errorf("invalid base64 image part: %+v", parts[1])
	}

	if parts[2].Type != "image" || parts[2].Source.MediaType != "image/png" {
		t.Errorf("invalid bytes image part: %+v", parts[2])
	}
}

func TestMessageBuilder_Errors(t *testing.T) {
	t.Run("invalid image bytes", func(t *testing.T) {
		_, err := client.NewMessage().
			ImageBytes([]byte("not an image")).
			Build()
		if err == nil {
			t.Error("expected error for invalid image bytes, got nil")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := client.NewMessage().
			ImageFile("non_existent_file.png").
			Build()
		if err == nil {
			t.Error("expected error for missing file, got nil")
		}
	})

	t.Run("invalid reader data", func(t *testing.T) {
		buf := bytes.NewReader([]byte("not an image"))
		_, err := client.NewMessage().
			ImageReader(buf).
			Build()
		if err == nil {
			t.Error("expected error for invalid reader data, got nil")
		}
	})
}
