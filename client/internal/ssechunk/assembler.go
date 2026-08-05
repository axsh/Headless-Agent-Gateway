// Package ssechunk reassembles chunked SSE tool_result_part events.
package ssechunk

import "fmt"

// Part holds one tool_result_part payload.
type Part struct {
	ChunkID    string
	ChunkIndex int
	ChunkTotal int
	Content    string
}

// Assembler buffers tool_result_part chunks until completion.
type Assembler struct {
	pending map[string]*chunkBuffer
}

type chunkBuffer struct {
	parts map[int]string
	total int
}

// NewAssembler returns an empty chunk assembler.
func NewAssembler() *Assembler {
	return &Assembler{pending: make(map[string]*chunkBuffer)}
}

// AddPart records a tool_result_part chunk.
func (a *Assembler) AddPart(p Part) error {
	if p.ChunkID == "" {
		return fmt.Errorf("tool_result_part missing chunk_id")
	}
	buf, ok := a.pending[p.ChunkID]
	if !ok {
		buf = &chunkBuffer{parts: make(map[int]string), total: p.ChunkTotal}
		a.pending[p.ChunkID] = buf
	}
	if buf.total != p.ChunkTotal {
		return fmt.Errorf("tool_result_part total mismatch")
	}
	if _, exists := buf.parts[p.ChunkIndex]; exists {
		return fmt.Errorf("duplicate chunk index %d", p.ChunkIndex)
	}
	buf.parts[p.ChunkIndex] = p.Content
	return nil
}

// Complete finishes a chunked tool_result and returns reassembled content.
func (a *Assembler) Complete(chunkID string) (string, error) {
	buf, ok := a.pending[chunkID]
	if !ok {
		return "", fmt.Errorf("unknown chunk_id %q", chunkID)
	}
	if len(buf.parts) != buf.total {
		return "", fmt.Errorf("incomplete tool_result chunks")
	}
	var assembled string
	for i := 0; i < buf.total; i++ {
		part, ok := buf.parts[i]
		if !ok {
			return "", fmt.Errorf("missing chunk index %d", i)
		}
		assembled += part
	}
	delete(a.pending, chunkID)
	return assembled, nil
}

// FlushIncomplete returns an error if any chunks remain incomplete.
func (a *Assembler) FlushIncomplete() error {
	if len(a.pending) > 0 {
		return fmt.Errorf("incomplete tool_result chunks")
	}
	return nil
}
