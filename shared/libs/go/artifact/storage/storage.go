// Package storage manages physical files for user-uploaded artifacts.
package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// WriteInfo contains metadata about a freshly written artifact file.
type WriteInfo struct {
	ActualPath string
	Size       int64
	SHA256     string
	MIMEType   string
}

// UserArtifactStorage manages files stored in a base directory.
// Each artifact is stored as <baseDir>/<id> where id is the UUID assigned by the caller.
type UserArtifactStorage struct {
	baseDir string
}

// New creates a UserArtifactStorage backed by baseDir.
// The directory is created if it does not exist.
func New(baseDir string) (*UserArtifactStorage, error) {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("create artifact storage dir: %w", err)
	}
	return &UserArtifactStorage{baseDir: baseDir}, nil
}

// ActualPath returns the absolute filesystem path for the given artifact ID.
func (s *UserArtifactStorage) ActualPath(id string) string {
	return filepath.Join(s.baseDir, id)
}

// Write stores the content from r under id, computing SHA256 and MIME type.
// Returns WriteInfo with the metadata.
func (s *UserArtifactStorage) Write(id string, r io.Reader) (*WriteInfo, error) {
	dest := s.ActualPath(id)
	f, err := os.Create(dest)
	if err != nil {
		return nil, fmt.Errorf("create artifact file: %w", err)
	}
	defer f.Close()

	h := sha256.New()

	// Peek the first 512 bytes for MIME type detection.
	buf := make([]byte, 512)
	n, peekErr := io.ReadFull(r, buf)
	if peekErr != nil && peekErr != io.ErrUnexpectedEOF && peekErr != io.EOF {
		return nil, fmt.Errorf("read artifact: %w", peekErr)
	}
	mimeType := http.DetectContentType(buf[:n])

	// Write the peeked bytes then the rest.
	multi := io.MultiWriter(f, h)
	if _, err := multi.Write(buf[:n]); err != nil {
		return nil, fmt.Errorf("write artifact: %w", err)
	}
	rest, err := io.Copy(multi, r)
	if err != nil {
		return nil, fmt.Errorf("write artifact: %w", err)
	}

	size := int64(n) + rest
	sha := hex.EncodeToString(h.Sum(nil))

	return &WriteInfo{
		ActualPath: dest,
		Size:       size,
		SHA256:     sha,
		MIMEType:   mimeType,
	}, nil
}

// Read opens the artifact file for reading.
// The caller is responsible for closing the returned ReadCloser.
func (s *UserArtifactStorage) Read(id string) (io.ReadCloser, error) {
	f, err := os.Open(s.ActualPath(id))
	if err != nil {
		return nil, fmt.Errorf("open artifact %s: %w", id, err)
	}
	return f, nil
}

// Delete removes the artifact file for id.
func (s *UserArtifactStorage) Delete(id string) error {
	if err := os.Remove(s.ActualPath(id)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete artifact %s: %w", id, err)
	}
	return nil
}
