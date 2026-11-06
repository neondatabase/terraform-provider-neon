package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_mustConvertStr(t *testing.T) {
	tests := map[string]any{
		"":      "",
		"42":    int64(42),
		"-2":    int64(-2),
		"3.14":  float64(3.14),
		"128MB": "128MB",
		"on":    "on",
		"off":   "off",
		"1s":    "1s",
		"...":   "...",
	}
	t.Parallel()
	for s, want := range tests {
		t.Run(s, func(t *testing.T) {
			assert.Equal(t, want, mustConvertStr(s))
		})
	}
}
