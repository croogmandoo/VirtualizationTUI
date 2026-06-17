package ui

import "testing"

func TestThemeByNameFallsBack(t *testing.T) {
	if got := ThemeByName("default").Name; got != "default" {
		t.Fatalf("default theme name = %q", got)
	}
	if got := ThemeByName("does-not-exist").Name; got != DefaultTheme {
		t.Fatalf("unknown theme should fall back to %q, got %q", DefaultTheme, got)
	}
}

func TestNextThemeCyclesAndWraps(t *testing.T) {
	names := ThemeNames()
	if len(names) < 2 {
		t.Skip("need at least two themes to test cycling")
	}
	// Walking NextTheme len(names) times from the first name returns to it.
	cur := names[0]
	seen := map[string]bool{cur: true}
	for i := 0; i < len(names); i++ {
		cur = NextTheme(cur)
		if i < len(names)-1 {
			seen[cur] = true
		}
	}
	if cur != names[0] {
		t.Fatalf("cycling through all themes should wrap to %q, got %q", names[0], cur)
	}
	if len(seen) != len(names) {
		t.Fatalf("expected to visit all %d themes, visited %d", len(names), len(seen))
	}
}

func TestNextThemeUnknownStartsCycle(t *testing.T) {
	if got := NextTheme("bogus"); got != ThemeNames()[0] {
		t.Fatalf("unknown current theme should start cycle at %q, got %q", ThemeNames()[0], got)
	}
}
