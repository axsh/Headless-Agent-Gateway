package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Canonical is the Tern shared session store at session_dir root
// ({work_dir}/.tern/{session_id} by default).
type Canonical struct {
	Dir string
}

// OpenCanonical returns a Canonical rooted at sessionDir.
func OpenCanonical(sessionDir string) *Canonical {
	return &Canonical{Dir: sessionDir}
}

// HistoryDir returns {session_dir}/history.
func (c *Canonical) HistoryDir() string {
	return filepath.Join(c.Dir, "history")
}

// NativeDir returns {session_dir}/native.
// Legacy path helper only; Init does not create this directory.
func (c *Canonical) NativeDir() string {
	return filepath.Join(c.Dir, "native")
}

// Init creates the canonical folder layout and metadata.json.
// Existing metadata keeps AgentBindings; ActiveAgent is synced when non-empty.
func (c *Canonical) Init(sessionID, activeAgent string) error {
	if c.Dir == "" {
		return fmt.Errorf("canonical init: session dir is empty")
	}
	if err := os.MkdirAll(c.HistoryDir(), 0755); err != nil {
		return fmt.Errorf("canonical init history: %w", err)
	}

	if meta, err := c.LoadMetadata(); err == nil {
		if activeAgent != "" && meta.ActiveAgent != activeAgent {
			meta.ActiveAgent = activeAgent
			meta.UpdatedAt = time.Now()
			return c.saveMetadata(meta)
		}
		return nil
	}

	now := time.Now()
	meta := &SessionMetadata{
		SessionID:     sessionID,
		Status:        StatusActive,
		Latest:        0,
		TotalSeq:      0,
		ContextStart:  0,
		CreatedAt:     now,
		UpdatedAt:     now,
		ActiveAgent:   activeAgent,
		AgentBindings: map[string]AgentBinding{},
	}
	return c.saveMetadata(meta)
}

// LoadMetadata reads metadata.json.
func (c *Canonical) LoadMetadata() (*SessionMetadata, error) {
	data, err := os.ReadFile(filepath.Join(c.Dir, "metadata.json"))
	if err != nil {
		return nil, fmt.Errorf("canonical load metadata: %w", err)
	}
	var meta SessionMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("canonical parse metadata: %w", err)
	}
	if meta.AgentBindings == nil {
		meta.AgentBindings = map[string]AgentBinding{}
	}
	return &meta, nil
}

func (c *Canonical) saveMetadata(meta *SessionMetadata) error {
	meta.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("canonical marshal metadata: %w", err)
	}
	return atomicWrite(filepath.Join(c.Dir, "metadata.json"), data)
}

// NextSeq returns the next unused history sequence number.
func (c *Canonical) NextSeq() (int, error) {
	meta, err := c.LoadMetadata()
	if err == nil && meta.TotalSeq > 0 {
		return meta.TotalSeq + 1, nil
	}
	maxSeq, err := maxHistorySeq(c.HistoryDir())
	if err != nil {
		return 0, err
	}
	return maxSeq + 1, nil
}

func maxHistorySeq(histDir string) (int, error) {
	entries, err := os.ReadDir(histDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	maxSeq := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		seq, parseErr := strconv.ParseInt(name, 16, 0)
		if parseErr != nil {
			continue
		}
		if int(seq) > maxSeq {
			maxSeq = int(seq)
		}
	}
	return maxSeq, nil
}

// Append writes messages to history/ and updates TotalSeq.
// Messages with Seq==0 receive consecutive sequence numbers starting at NextSeq.
func (c *Canonical) Append(msgs []Message) error {
	if c.Dir == "" {
		return nil
	}
	if err := os.MkdirAll(c.HistoryDir(), 0755); err != nil {
		return fmt.Errorf("canonical mkdir history: %w", err)
	}
	if _, err := c.LoadMetadata(); err != nil {
		if initErr := c.Init("", ""); initErr != nil {
			return initErr
		}
	}

	next, err := c.NextSeq()
	if err != nil {
		return err
	}
	assigned := make([]Message, len(msgs))
	for i, msg := range msgs {
		if msg.Seq == 0 {
			msg.Seq = next
			next++
		}
		msg.Origin = NormalizeOrigin(msg.Origin)
		if msg.Timestamp.IsZero() {
			msg.Timestamp = time.Now()
		}
		assigned[i] = msg
	}
	if err := AppendHistory(c.HistoryDir(), assigned); err != nil {
		return err
	}

	meta, err := c.LoadMetadata()
	if err != nil {
		return err
	}
	maxSeq := meta.TotalSeq
	for _, msg := range assigned {
		if msg.Seq > maxSeq {
			maxSeq = msg.Seq
		}
	}
	meta.TotalSeq = maxSeq
	meta.Latest = maxSeq
	return c.saveMetadata(meta)
}

// LoadRange loads history entries in [fromSeq, toSeq] inclusive.
// toSeq<=0 means through TotalSeq (or the highest history file).
func (c *Canonical) LoadRange(fromSeq, toSeq int) ([]Message, error) {
	if toSeq <= 0 {
		meta, err := c.LoadMetadata()
		if err == nil && meta.TotalSeq > 0 {
			toSeq = meta.TotalSeq
		} else {
			maxSeq, scanErr := maxHistorySeq(c.HistoryDir())
			if scanErr != nil {
				return nil, scanErr
			}
			toSeq = maxSeq
		}
	}
	if toSeq < fromSeq {
		return nil, nil
	}
	return LoadHistory(c.HistoryDir(), fromSeq, toSeq)
}

// LoadHistoryFrom loads history from fromSeq through the latest seq.
func (c *Canonical) LoadHistoryFrom(fromSeq int) ([]Message, error) {
	return c.LoadRange(fromSeq, 0)
}

// UpdateBinding records the native session id and ingest watermark for an agent.
// An empty nativeSessionID keeps the previously stored AgentSessionID.
func (c *Canonical) UpdateBinding(agent, nativeSessionID string, throughSeq int) error {
	meta, err := c.LoadMetadata()
	if err != nil {
		if initErr := c.Init("", agent); initErr != nil {
			return initErr
		}
		meta, err = c.LoadMetadata()
		if err != nil {
			return err
		}
	}
	agent = NormalizeOrigin(agent)
	existing := meta.AgentBindings[agent]
	if nativeSessionID == "" {
		nativeSessionID = existing.AgentSessionID
	}
	meta.AgentBindings[agent] = AgentBinding{
		AgentSessionID:     nativeSessionID,
		IngestedThroughSeq: throughSeq,
	}
	return c.saveMetadata(meta)
}

// SetActiveAgent updates metadata.active_agent without touching bindings.
func (c *Canonical) SetActiveAgent(agent string) error {
	meta, err := c.LoadMetadata()
	if err != nil {
		return err
	}
	meta.ActiveAgent = agent
	return c.saveMetadata(meta)
}

// SetSupplement merges non-empty fields of s into the stored strategy.
func (c *Canonical) SetSupplement(s SupplementStrategy) error {
	meta, err := c.LoadMetadata()
	if err != nil {
		return err
	}
	meta.Supplement = overlaySupplement(meta.Supplement, s)
	return c.saveMetadata(meta)
}

func overlaySupplement(base, over SupplementStrategy) SupplementStrategy {
	if over.Algorithm != "" {
		base.Algorithm = over.Algorithm
	}
	if over.Model != "" {
		base.Model = over.Model
	}
	if over.MaxChunkMessages != 0 {
		base.MaxChunkMessages = over.MaxChunkMessages
	}
	if over.ThresholdBytes != 0 {
		base.ThresholdBytes = over.ThresholdBytes
	}
	if over.RecentKeep != 0 {
		base.RecentKeep = over.RecentKeep
	}
	return base
}
