package goagent_test

import (
	"testing"

	"github.com/Germanblandin1/goagent"
)

func TestModelInfo_IsFree(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		info goagent.ModelInfo
		want bool
	}{
		{
			name: "nil pricing is unknown, not free",
			info: goagent.ModelInfo{Name: "m", Pricing: nil},
			want: false,
		},
		{
			name: "all-zero pricing is free",
			info: goagent.ModelInfo{Name: "m", Pricing: &goagent.Pricing{Currency: "USD"}},
			want: true,
		},
		{
			name: "non-zero input price is not free",
			info: goagent.ModelInfo{Name: "m", Pricing: &goagent.Pricing{InputPerMTok: 3}},
			want: false,
		},
		{
			name: "non-zero output price is not free",
			info: goagent.ModelInfo{Name: "m", Pricing: &goagent.Pricing{OutputPerMTok: 15}},
			want: false,
		},
		{
			name: "non-zero per-request fee is not free",
			info: goagent.ModelInfo{Name: "m", Pricing: &goagent.Pricing{RequestUSD: 0.01}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.info.IsFree(); got != tt.want {
				t.Errorf("IsFree() = %v, want %v", got, tt.want)
			}
		})
	}
}
