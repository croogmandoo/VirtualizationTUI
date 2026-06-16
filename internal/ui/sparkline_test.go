package ui

import (
	"testing"
	"unicode/utf8"
)

func TestSparklineEmpty(t *testing.T) {
	if got := Sparkline(nil); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestSparklineLength(t *testing.T) {
	in := []float64{0, 1, 2, 3, 4, 5, 6, 7}
	got := Sparkline(in)
	if n := utf8.RuneCountInString(got); n != len(in) {
		t.Fatalf("expected %d runes, got %d (%q)", len(in), n, got)
	}
}

func TestSparklineExtremes(t *testing.T) {
	got := Sparkline([]float64{0, 100})
	runes := []rune(got)
	if runes[0] != sparkBlocks[0] {
		t.Errorf("min should map to lowest block, got %q", string(runes[0]))
	}
	if runes[1] != sparkBlocks[len(sparkBlocks)-1] {
		t.Errorf("max should map to highest block, got %q", string(runes[1]))
	}
}

func TestSparklineFlat(t *testing.T) {
	// All-equal values must not panic (zero span) and should render lowest block.
	got := Sparkline([]float64{5, 5, 5})
	for _, r := range got {
		if r != sparkBlocks[0] {
			t.Fatalf("flat data should render lowest block, got %q", got)
		}
	}
}
