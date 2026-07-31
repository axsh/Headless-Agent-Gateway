package storage_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/artifact/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStorage(t *testing.T) *storage.UserArtifactStorage {
	t.Helper()
	s, err := storage.New(t.TempDir())
	require.NoError(t, err)
	return s
}

func TestStorage_WriteAndRead(t *testing.T) {
	s := newTestStorage(t)

	info, err := s.Write("uuid-1", strings.NewReader("hello world"))
	require.NoError(t, err)
	assert.Equal(t, int64(11), info.Size)
	assert.NotEmpty(t, info.SHA256)

	rc, err := s.Read("uuid-1")
	require.NoError(t, err)
	defer rc.Close()

	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(data))
}

func TestStorage_SHA256_Deterministic(t *testing.T) {
	s := newTestStorage(t)

	info1, _ := s.Write("a", strings.NewReader("data"))
	info2, _ := s.Write("b", strings.NewReader("data"))
	assert.Equal(t, info1.SHA256, info2.SHA256, "same content → same SHA")
}

func TestStorage_Delete(t *testing.T) {
	s := newTestStorage(t)
	_, err := s.Write("uuid-del", strings.NewReader("content"))
	require.NoError(t, err)

	require.NoError(t, s.Delete("uuid-del"))

	_, err = s.Read("uuid-del")
	assert.Error(t, err, "reading deleted file should fail")
}

func TestStorage_Read_NotFound(t *testing.T) {
	s := newTestStorage(t)
	_, err := s.Read("nonexistent")
	assert.Error(t, err)
}

func TestStorage_DetectMIME_Text(t *testing.T) {
	s := newTestStorage(t)
	info, err := s.Write("txt", strings.NewReader("plain text content"))
	require.NoError(t, err)
	assert.Contains(t, info.MIMEType, "text/plain")
}

func TestStorage_DetectMIME_PNG(t *testing.T) {
	// Minimal PNG header bytes.
	pngHeader := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	}
	s := newTestStorage(t)
	info, err := s.Write("img", bytes.NewReader(pngHeader))
	require.NoError(t, err)
	assert.Equal(t, "image/png", info.MIMEType)
}

func TestStorage_ActualPath(t *testing.T) {
	s := newTestStorage(t)
	_, err := s.Write("pathtest", strings.NewReader("x"))
	require.NoError(t, err)

	p := s.ActualPath("pathtest")
	assert.NotEmpty(t, p)
	assert.True(t, strings.HasSuffix(p, "pathtest"))
}
