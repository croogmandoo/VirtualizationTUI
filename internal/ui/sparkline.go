package ui

import "strings"

// sparkBlocks are the eight Unicode block-elements used for sparklines, low→high.
var sparkBlocks = []rune("▁▂▃▄▅▆▇█")

// Sparkline renders a slice of values as a Unicode sparkline string. An empty
// input yields an empty string. The scale is relative to the min/max of the data.
func Sparkline(values []float64) string {
	if len(values) == 0 {
		return ""
	}
	min, max := values[0], values[0]
	for _, v := range values {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	span := max - min
	var b strings.Builder
	for _, v := range values {
		var idx int
		if span > 0 {
			idx = int((v - min) / span * float64(len(sparkBlocks)-1))
		}
		if idx < 0 {
			idx = 0
		}
		if idx >= len(sparkBlocks) {
			idx = len(sparkBlocks) - 1
		}
		b.WriteRune(sparkBlocks[idx])
	}
	return b.String()
}
