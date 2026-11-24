package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestTimeoutFromEnv(t *testing.T) {
	valid := []struct {
		raw  string
		want time.Duration
	}{
		{"", 2 * time.Second},
		{"2s", 2 * time.Second},
		{"50ms", 50 * time.Millisecond},
		{"1ms", time.Millisecond},
		{"1500ms", 1500 * time.Millisecond},
	}
	for _, tt := range valid {
		t.Run("valid "+tt.raw, func(t *testing.T) {
			got, err := requestTimeoutFromEnv(tt.raw)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	for _, raw := range []string{"abc", "0", "-1s", "10"} {
		t.Run("invalid "+raw, func(t *testing.T) {
			_, err := requestTimeoutFromEnv(raw)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "REQUEST_TIMEOUT")
		})
	}
}
