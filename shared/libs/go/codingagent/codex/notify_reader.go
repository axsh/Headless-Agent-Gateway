package codex

import (
	"bufio"
	"context"
	"io"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

// FanInAppServerNotifications reads JSONL notifications from r and sends parsed
// StreamEvents to out until ctx is done or r reaches EOF. Does not close out.
func FanInAppServerNotifications(ctx context.Context, r io.Reader, out chan<- codingagent.StreamEvent) {
	if r == nil || out == nil {
		return
	}
	sc := bufio.NewScanner(r)
	// Allow large unified diffs in a single notification line.
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 8*1024*1024)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if !sc.Scan() {
			return
		}
		ev := ParseAppServerNotification(sc.Text())
		if ev == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case out <- *ev:
		}
	}
}
