package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadCappedInputUnderCap(t *testing.T) {
	t.Parallel()
	in := strings.NewReader("hello world")
	got, err := readCappedInput(in, 1024)
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(got))
}

func TestReadCappedInputAtCap(t *testing.T) {
	t.Parallel()
	// Exactly maxBytes must still succeed; the +1-byte probe is for "over".
	payload := bytes.Repeat([]byte("x"), 1024)
	got, err := readCappedInput(bytes.NewReader(payload), 1024)
	require.NoError(t, err, "unexpected error at exact cap")
	assert.Len(t, got, 1024)
}

func TestReadCappedInputOverCap(t *testing.T) {
	t.Parallel()
	payload := bytes.Repeat([]byte("x"), 1025)
	_, err := readCappedInput(bytes.NewReader(payload), 1024)
	require.ErrorIs(t, err, errCountInputTooLarge)
}

func TestCountMaxInputBytesConstant(t *testing.T) {
	t.Parallel()
	require.Positive(t, countMaxInputBytes, "countMaxInputBytes must be positive")
	// A cap below 1 MiB would reject realistic documents; above 1 GiB
	// defeats the OOM-protection rationale. Anchors the choice to a
	// sensible band so a future tweak surfaces as a test failure rather
	// than silent drift.
	const (
		minSane = 1 << 20
		maxSane = 1 << 30
	)
	assert.GreaterOrEqual(t, countMaxInputBytes, minSane,
		"countMaxInputBytes below sane lower bound")
	assert.LessOrEqual(t, countMaxInputBytes, maxSane,
		"countMaxInputBytes above sane upper bound")
}
