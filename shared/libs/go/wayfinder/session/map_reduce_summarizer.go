package session

// SummarizeFunc is a function that summarizes a slice of messages into text.
// Uses a function type to avoid cyclic imports with the wayfinder package.
type SummarizeFunc func(msgs []Message) (string, error)

// MergeFunc merges two summary texts into one.
type MergeFunc func(summaryA, summaryB string) (string, error)

// MapReduceSummarizer implements chunked summarization using a Map&Reduce approach.
// Instead of summarizing all old messages in a single LLM call (which may exceed
// the context window), it splits messages into small chunks, summarizes each chunk
// independently, then merges the summaries pairwise until a single summary remains.
type MapReduceSummarizer struct {
	summarize       SummarizeFunc
	merge           MergeFunc
	fallbackSummary func([]Message) string
	maxChunkMsgs    int // Max messages per chunk (default: 20).
}

// NewMapReduceSummarizer creates a new MapReduceSummarizer.
func NewMapReduceSummarizer(
	summarize SummarizeFunc,
	merge MergeFunc,
	fallback func([]Message) string,
	maxChunkMsgs int,
) *MapReduceSummarizer {
	if maxChunkMsgs <= 0 {
		maxChunkMsgs = 20
	}
	return &MapReduceSummarizer{
		summarize:       summarize,
		merge:           merge,
		fallbackSummary: fallback,
		maxChunkMsgs:    maxChunkMsgs,
	}
}

// Summarize performs Map&Reduce summarization:
//  1. Map: split messages into chunks and summarize each independently
//  2. Reduce: merge chunk summaries pairwise until one summary remains
func (s *MapReduceSummarizer) Summarize(msgs []Message) (string, error) {
	// 1. Map: split into chunks.
	chunks := s.splitIntoChunks(msgs)

	// 2. Map: summarize each chunk independently.
	summaries := make([]string, len(chunks))
	for i, chunk := range chunks {
		summary, err := s.summarize(chunk)
		if err != nil {
			// Fallback: use structured summary for this chunk only.
			summaries[i] = s.fallbackSummary(chunk)
		} else {
			summaries[i] = summary
		}
	}

	// 3. Reduce: merge summaries pairwise.
	return s.reduceSummaries(summaries)
}

// splitIntoChunks divides messages into 1-4 chunks respecting tool pair boundaries.
func (s *MapReduceSummarizer) splitIntoChunks(msgs []Message) [][]Message {
	if len(msgs) == 0 {
		return nil
	}

	// Calculate chunk count (1-4).
	chunkCount := (len(msgs) + s.maxChunkMsgs - 1) / s.maxChunkMsgs
	if chunkCount < 1 {
		chunkCount = 1
	}
	if chunkCount > 4 {
		chunkCount = 4
	}

	if chunkCount == 1 {
		return [][]Message{msgs}
	}

	// Calculate boundaries.
	chunkSize := len(msgs) / chunkCount
	boundaries := make([]int, chunkCount-1)
	for i := range chunkCount - 1 {
		boundaries[i] = chunkSize * (i + 1)
	}

	// Adjust boundaries to respect tool pair integrity.
	for i := range boundaries {
		boundaries[i] = adjustBoundaryForToolPairs(msgs, boundaries[i])
	}

	// Build chunks from boundaries.
	chunks := make([][]Message, 0, chunkCount)
	prev := 0
	for _, boundary := range boundaries {
		if boundary <= prev {
			continue // Skip degenerate boundaries.
		}
		chunks = append(chunks, msgs[prev:boundary])
		prev = boundary
	}
	// Last chunk: remaining messages.
	if prev < len(msgs) {
		chunks = append(chunks, msgs[prev:])
	}

	return chunks
}

// reduceSummaries performs pairwise reduction of chunk summaries.
// [A, B, C, D] -> [merge(A,B), merge(C,D)] -> [merge(AB, CD)]
func (s *MapReduceSummarizer) reduceSummaries(summaries []string) (string, error) {
	if len(summaries) == 0 {
		return "", nil
	}
	if len(summaries) == 1 {
		return summaries[0], nil
	}

	for len(summaries) > 1 {
		var next []string
		for i := 0; i < len(summaries); i += 2 {
			if i+1 < len(summaries) {
				merged, err := s.merge(summaries[i], summaries[i+1])
				if err != nil {
					// Fallback: plain concatenation with separator.
					merged = summaries[i] + "\n---\n" + summaries[i+1]
				}
				next = append(next, merged)
			} else {
				// Odd element: carry forward.
				next = append(next, summaries[i])
			}
		}
		summaries = next
	}
	return summaries[0], nil
}
