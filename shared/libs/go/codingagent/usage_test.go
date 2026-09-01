package codingagent_test

import (
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func TestAddUsage(t *testing.T) {
	cost1 := 0.01
	cost2 := 0.02
	tests := []struct {
		name string
		dst  codingagent.TokenUsage
		src  codingagent.TokenUsage
		want codingagent.TokenUsage
	}{
		{
			name: "sums input and output",
			dst:  codingagent.TokenUsage{InputTokens: 10, OutputTokens: 3},
			src:  codingagent.TokenUsage{InputTokens: 5, OutputTokens: 2},
			want: codingagent.TokenUsage{InputTokens: 15, OutputTokens: 5},
		},
		{
			name: "sums cache fields",
			dst:  codingagent.TokenUsage{CachedInputTokens: 100, CacheCreationInputTokens: 10},
			src:  codingagent.TokenUsage{CachedInputTokens: 50, CacheCreationInputTokens: 5},
			want: codingagent.TokenUsage{CachedInputTokens: 150, CacheCreationInputTokens: 15},
		},
		{
			name: "sums total_tokens only when src has value",
			dst:  codingagent.TokenUsage{TotalTokens: 0},
			src:  codingagent.TokenUsage{TotalTokens: 42},
			want: codingagent.TokenUsage{TotalTokens: 42},
		},
		{
			name: "does not invent total_tokens from input+output",
			dst:  codingagent.TokenUsage{InputTokens: 10, OutputTokens: 5},
			src:  codingagent.TokenUsage{InputTokens: 1, OutputTokens: 1},
			want: codingagent.TokenUsage{InputTokens: 11, OutputTokens: 6, TotalTokens: 0},
		},
		{
			name: "sums cost when both present",
			dst:  codingagent.TokenUsage{TotalCostUSD: &cost1},
			src:  codingagent.TokenUsage{TotalCostUSD: &cost2},
			want: codingagent.TokenUsage{TotalCostUSD: floatPtr(0.03)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dst := tt.dst
			codingagent.AddUsage(&dst, tt.src)
			if dst.InputTokens != tt.want.InputTokens || dst.OutputTokens != tt.want.OutputTokens {
				t.Errorf("tokens = in %d out %d, want in %d out %d",
					dst.InputTokens, dst.OutputTokens, tt.want.InputTokens, tt.want.OutputTokens)
			}
			if dst.CachedInputTokens != tt.want.CachedInputTokens ||
				dst.CacheCreationInputTokens != tt.want.CacheCreationInputTokens {
				t.Errorf("cache = %d/%d, want %d/%d",
					dst.CachedInputTokens, dst.CacheCreationInputTokens,
					tt.want.CachedInputTokens, tt.want.CacheCreationInputTokens)
			}
			if dst.TotalTokens != tt.want.TotalTokens {
				t.Errorf("TotalTokens = %d, want %d", dst.TotalTokens, tt.want.TotalTokens)
			}
			if tt.want.TotalCostUSD == nil {
				if dst.TotalCostUSD != nil {
					t.Errorf("TotalCostUSD = %v, want nil", *dst.TotalCostUSD)
				}
			} else if dst.TotalCostUSD == nil || *dst.TotalCostUSD != *tt.want.TotalCostUSD {
				got := any(nil)
				if dst.TotalCostUSD != nil {
					got = *dst.TotalCostUSD
				}
				t.Errorf("TotalCostUSD = %v, want %v", got, *tt.want.TotalCostUSD)
			}
		})
	}
}

func floatPtr(v float64) *float64 { return &v }

func TestAddUsage_NilDst(t *testing.T) {
	codingagent.AddUsage(nil, codingagent.TokenUsage{InputTokens: 1})
}

func TestFilterTurnUsage_LastNAndAfter(t *testing.T) {
	turns := []codingagent.TurnUsageRecord{
		{TurnID: "t1", Usage: codingagent.TokenUsage{InputTokens: 1, OutputTokens: 1}},
		{TurnID: "t2", Usage: codingagent.TokenUsage{InputTokens: 2, OutputTokens: 2}},
		{TurnID: "t3", Usage: codingagent.TokenUsage{InputTokens: 3, OutputTokens: 3}},
	}
	got := codingagent.FilterTurnUsage(turns, codingagent.UsageQuery{LastN: 1})
	if len(got) != 1 || got[0].TurnID != "t3" {
		t.Fatalf("LastN=1 got %+v", got)
	}
	got = codingagent.FilterTurnUsage(turns, codingagent.UsageQuery{AfterTurnID: "t1"})
	if len(got) != 2 || got[0].TurnID != "t2" {
		t.Fatalf("AfterTurnID got %+v", got)
	}
	sum := codingagent.SumTurnUsage(got)
	if sum.InputTokens != 5 || sum.OutputTokens != 5 {
		t.Fatalf("sum = %+v", sum)
	}
}

func TestFilterTurnUsage_Since(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	turns := []codingagent.TurnUsageRecord{
		{TurnID: "t1", EndedAt: t0, Usage: codingagent.TokenUsage{InputTokens: 1}},
		{TurnID: "t2", EndedAt: t0.Add(time.Hour), Usage: codingagent.TokenUsage{InputTokens: 2}},
	}
	got := codingagent.FilterTurnUsage(turns, codingagent.UsageQuery{Since: t0.Add(30 * time.Minute)})
	if len(got) != 1 || got[0].TurnID != "t2" {
		t.Fatalf("got %+v", got)
	}
}
