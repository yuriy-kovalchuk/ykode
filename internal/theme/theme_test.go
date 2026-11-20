package theme

import (
	"strconv"
	"testing"
)

// every role of every palette is a colour a terminal can draw: hex, or one of its sixteen
func TestEveryPaletteColoursEveryRole(t *testing.T) {
	for _, name := range Names()[1:] {
		p, ok := Named(name)
		if !ok {
			t.Fatalf("%s is listed but not found", name)
		}
		roles := map[string]string{"accent": p.Accent, "cyan": p.Cyan, "ok": p.OK, "warn": p.Warn, "bad": p.Bad,
			"tool": p.Tool, "muted": p.Muted, "think": p.Think, "border": p.Border,
			"added word": p.AddWordGround, "removed word": p.DelWordGround}
		if p.Background != "" {
			roles["background"], roles["foreground"] = p.Background, p.Foreground
			roles["added row"], roles["removed row"] = p.AddGround, p.DelGround
		}
		for role, c := range roles {
			if !drawable(c) {
				t.Errorf("%s: %s is %q, not a colour", name, role, c)
			}
		}
	}
}

func drawable(c string) bool {
	if _, ok := rgb(c); ok {
		return true
	}
	n, err := strconv.Atoi(c)
	return err == nil && n >= 0 && n < 16
}

// a name picks one palette: auto comes first and no two palettes share a name
func TestEveryNameIsOnePalette(t *testing.T) {
	names := Names()
	if names[0] != Auto {
		t.Errorf("the first choice is %q, want auto", names[0])
	}
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			t.Errorf("%s is listed twice", name)
		}
		seen[name] = true
	}
	if err := Check("neon"); err == nil {
		t.Error("a theme ykode does not have was accepted")
	}
	if err := Check(Auto); err != nil {
		t.Errorf("auto was refused: %v", err)
	}
}

// auto is ykode's own pair, chosen by the shade of the terminal's background
func TestAutoIsYkodesPaletteForTheBackground(t *testing.T) {
	if p := For(true); p.Name != "ykode-dark" || !p.Dark {
		t.Errorf("a dark background got %s", p.Name)
	}
	if p := For(false); p.Name != "ykode-light" || p.Dark {
		t.Errorf("a light background got %s", p.Name)
	}
}

// a change's grounds a theme left out are its background moved toward its green and red
func TestTheGroundsOfAChangeAreMixedFromTheTheme(t *testing.T) {
	p, _ := Named("dracula")
	if p.AddGround == "" || p.AddGround == p.AddWordGround {
		t.Fatalf("grounds = %q and %q; want a row ground and a stronger word ground", p.AddGround, p.AddWordGround)
	}
	if got := mix("#000000", "#ffffff", 0.5); got != "#808080" {
		t.Errorf("halfway from black to white is %s", got)
	}
	if got := mix("#282a36", "#50fa7b", 0); got != "#282a36" {
		t.Errorf("none of the way is %s, want the background itself", got)
	}
	if ykode, _ := Named("ykode-dark"); ykode.AddGround != "#1f4d2a" {
		t.Errorf("a ground a theme names was mixed over: %s", ykode.AddGround)
	}
}
