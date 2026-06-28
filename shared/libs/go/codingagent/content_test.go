package codingagent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractText_TextOnly(t *testing.T) {
	parts := []ContentPart{
		{Type: "text", Text: "Hello, "},
		{Type: "text", Text: "world!"},
	}
	result := ExtractText(parts)
	assert.Equal(t, "Hello, world!", result)
}

func TestExtractText_Mixed(t *testing.T) {
	parts := []ContentPart{
		{Type: "text", Text: "Look at this: "},
		{Type: "image", Source: &ImageSource{Type: "base64", MediaType: "image/png", Data: "iVBOR..."}},
		{Type: "text", Text: "What do you see?"},
	}
	result := ExtractText(parts)
	assert.Equal(t, "Look at this: What do you see?", result)
}

func TestExtractText_EmptyParts(t *testing.T) {
	result := ExtractText([]ContentPart{})
	assert.Equal(t, "", result)
}

func TestExtractText_NilParts(t *testing.T) {
	result := ExtractText(nil)
	assert.Equal(t, "", result)
}

func TestExtractText_EmptyTextBlocks(t *testing.T) {
	parts := []ContentPart{
		{Type: "text", Text: ""},
		{Type: "text", Text: ""},
	}
	result := ExtractText(parts)
	assert.Equal(t, "", result)
}

func TestHasNonTextContent(t *testing.T) {
	tests := []struct {
		name     string
		parts    []ContentPart
		expected bool
	}{
		{
			name: "text only",
			parts: []ContentPart{
				{Type: "text", Text: "Hello"},
			},
			expected: false,
		},
		{
			name: "image present",
			parts: []ContentPart{
				{Type: "text", Text: "Hello"},
				{Type: "image", Source: &ImageSource{Type: "base64", MediaType: "image/png", Data: "data"}},
			},
			expected: true,
		},
		{
			name:     "empty parts",
			parts:    []ContentPart{},
			expected: false,
		},
		{
			name:     "nil parts",
			parts:    nil,
			expected: false,
		},
		{
			name: "unknown type",
			parts: []ContentPart{
				{Type: "video"},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HasNonTextContent(tt.parts)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTextOnlyContent(t *testing.T) {
	result := TextOnlyContent("Hello, world!")
	assert.Len(t, result, 1)
	assert.Equal(t, "text", result[0].Type)
	assert.Equal(t, "Hello, world!", result[0].Text)
	assert.Nil(t, result[0].Source)
}

func TestValidateContentParts(t *testing.T) {
	tests := []struct {
		name    string
		parts   []ContentPart
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid text only",
			parts: []ContentPart{
				{Type: "text", Text: "Hello"},
			},
			wantErr: false,
		},
		{
			name: "valid image",
			parts: []ContentPart{
				{Type: "image", Source: &ImageSource{Type: "base64", MediaType: "image/png", Data: "iVBOR"}},
			},
			wantErr: false,
		},
		{
			name: "valid mixed",
			parts: []ContentPart{
				{Type: "text", Text: "Look at this:"},
				{Type: "image", Source: &ImageSource{Type: "base64", MediaType: "image/png", Data: "iVBOR"}},
			},
			wantErr: false,
		},
		{
			name:    "empty parts",
			parts:   []ContentPart{},
			wantErr: true,
			errMsg:  "content must not be empty",
		},
		{
			name:    "nil parts",
			parts:   nil,
			wantErr: true,
			errMsg:  "content must not be empty",
		},
		{
			name: "invalid type",
			parts: []ContentPart{
				{Type: "video"},
			},
			wantErr: true,
			errMsg:  "unsupported content type",
		},
		{
			name: "empty type",
			parts: []ContentPart{
				{Type: ""},
			},
			wantErr: true,
			errMsg:  "content type must not be empty",
		},
		{
			name: "image missing source",
			parts: []ContentPart{
				{Type: "image"},
			},
			wantErr: true,
			errMsg:  "image content part requires source",
		},
		{
			name: "image empty data",
			parts: []ContentPart{
				{Type: "image", Source: &ImageSource{Type: "base64", MediaType: "image/png", Data: ""}},
			},
			wantErr: true,
			errMsg:  "image source data must not be empty",
		},
		{
			name: "image empty media_type",
			parts: []ContentPart{
				{Type: "image", Source: &ImageSource{Type: "base64", MediaType: "", Data: "iVBOR"}},
			},
			wantErr: true,
			errMsg:  "image source media_type must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateContentParts(tt.parts)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
