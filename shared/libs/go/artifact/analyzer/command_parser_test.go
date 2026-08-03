package analyzer_test

import (
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/artifact/analyzer"
	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
	"github.com/stretchr/testify/assert"
)

func TestParseShellCommand(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want []analyzer.ParsedFileOp
	}{
		{
			name: "echo redirect create",
			cmd:  "echo hello > output.txt",
			want: []analyzer.ParsedFileOp{{Path: "output.txt", Operation: store.OperationCreate}},
		},
		{
			name: "append redirect update",
			cmd:  "echo x >> log.txt",
			want: []analyzer.ParsedFileOp{{Path: "log.txt", Operation: store.OperationUpdate}},
		},
		{
			name: "tee create",
			cmd:  "echo hi | tee out.txt",
			want: []analyzer.ParsedFileOp{{Path: "out.txt", Operation: store.OperationCreate}},
		},
		{
			name: "heredoc create",
			cmd:  "cat <<EOF > f.txt\nhi\nEOF",
			want: []analyzer.ParsedFileOp{{Path: "f.txt", Operation: store.OperationCreate}},
		},
		{
			name: "cp create",
			cmd:  "cp src.txt dst.txt",
			want: []analyzer.ParsedFileOp{{Path: "dst.txt", Operation: store.OperationCreate}},
		},
		{
			name: "mv update",
			cmd:  "mv old.txt new.txt",
			want: []analyzer.ParsedFileOp{{Path: "new.txt", Operation: store.OperationUpdate}},
		},
		{
			name: "touch create",
			cmd:  "touch newfile.txt",
			want: []analyzer.ParsedFileOp{{Path: "newfile.txt", Operation: store.OperationCreate}},
		},
		{
			name: "rm delete",
			cmd:  "rm obsolete.txt",
			want: []analyzer.ParsedFileOp{{Path: "obsolete.txt", Operation: store.OperationDelete}},
		},
		{
			name: "ls no op",
			cmd:  "ls -la",
			want: nil,
		},
		{
			name: "git status no op",
			cmd:  "git status",
			want: nil,
		},
		{
			name: "set-content create",
			cmd:  "Set-Content -Path out.txt -Value hi",
			want: []analyzer.ParsedFileOp{{Path: "out.txt", Operation: store.OperationCreate}},
		},
		{
			name: "out-file create",
			cmd:  "'hi' | Out-File out.txt",
			want: []analyzer.ParsedFileOp{{Path: "out.txt", Operation: store.OperationCreate}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := analyzer.ParseShellCommand(tc.cmd)
			if tc.want == nil {
				assert.Empty(t, got)
				return
			}
			assert.Len(t, got, len(tc.want))
			for i, w := range tc.want {
				assert.Equal(t, w.Path, got[i].Path)
				assert.Equal(t, w.Operation, got[i].Operation)
			}
		})
	}
}

func TestExtractShellCommand_LegacyShell(t *testing.T) {
	cmd := analyzer.ExtractShellCommand("shell", map[string]any{
		"arguments": `{"command":"echo hi > legacy.txt"}`,
	})
	assert.Equal(t, `echo hi > legacy.txt`, cmd)
}
