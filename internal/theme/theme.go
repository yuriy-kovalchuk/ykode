// Package theme is every palette the screen can draw with, as names and hex, so settings and screen agree
package theme

import (
	"fmt"
	"strconv"
)

// Auto picks ykode's dark or light palette from the background the terminal reports.
const Auto = "auto"

// Palette is one theme: the screen's roles as colours, hex or an ANSI index, "" for the terminal's own
type Palette struct {
	Name string
	Dark bool // drawn on a dark background, which also decides the input box's own shades
	// Background and Foreground repaint the terminal while the screen is up; "" leaves its own
	Background, Foreground                                  string
	Accent, Cyan, OK, Warn, Bad, Tool, Muted, Think, Border string
	// the four grounds a change is drawn on; "" mixes them from Background and OK or Bad
	AddGround, DelGround, AddWordGround, DelWordGround string
}

// palettes are the built-in themes, in the order a list shows them.
var palettes = []Palette{
	{Name: "ykode-dark", Dark: true,
		Accent: "#A78BFA", Cyan: "#67E8F9", OK: "#4ADE80", Warn: "#FBBF24", Bad: "#F87171", Tool: "#F472B6",
		Muted: "#7C8394", Think: "#9D8CD6", Border: "#454B61",
		AddGround: "#1f4d2a", DelGround: "#5c2630", AddWordGround: "#2f7d45", DelWordGround: "#93404d"},
	{Name: "ykode-light",
		Accent: "#6D28D9", Cyan: "#0E7490", OK: "#15803D", Warn: "#B45309", Bad: "#B91C1C", Tool: "#BE185D",
		Muted: "#8A8F98", Think: "#7C6BB8", Border: "#C9CCD6",
		AddGround: "#d9f2dd", DelGround: "#fbdfe2", AddWordGround: "#a4e0af", DelWordGround: "#f3adb5"},
	// the terminal's sixteen colours, so the screen follows whatever scheme the terminal already has
	{Name: "terminal", Dark: true,
		Accent: "5", Cyan: "6", OK: "2", Warn: "3", Bad: "1", Tool: "13", Muted: "8", Think: "4", Border: "8",
		AddWordGround: "2", DelWordGround: "1"},
	{Name: "dracula", Dark: true, Background: "#282a36", Foreground: "#f8f8f2",
		Accent: "#bd93f9", Cyan: "#8be9fd", OK: "#50fa7b", Warn: "#ffb86c", Bad: "#ff5555", Tool: "#ff79c6",
		Muted: "#6272a4", Think: "#bd93f9", Border: "#44475a"},
	{Name: "nord", Dark: true, Background: "#2e3440", Foreground: "#d8dee9",
		Accent: "#88c0d0", Cyan: "#8fbcbb", OK: "#a3be8c", Warn: "#ebcb8b", Bad: "#bf616a", Tool: "#b48ead",
		Muted: "#616e88", Think: "#81a1c1", Border: "#4c566a"},
	{Name: "solarized-dark", Dark: true, Background: "#002b36", Foreground: "#839496",
		Accent: "#268bd2", Cyan: "#2aa198", OK: "#859900", Warn: "#b58900", Bad: "#dc322f", Tool: "#d33682",
		Muted: "#586e75", Think: "#6c71c4", Border: "#073642"},
	{Name: "solarized-light", Background: "#fdf6e3", Foreground: "#657b83",
		Accent: "#268bd2", Cyan: "#2aa198", OK: "#859900", Warn: "#b58900", Bad: "#dc322f", Tool: "#d33682",
		Muted: "#93a1a1", Think: "#6c71c4", Border: "#eee8d5"},
	{Name: "gruvbox-dark", Dark: true, Background: "#282828", Foreground: "#ebdbb2",
		Accent: "#fabd2f", Cyan: "#8ec07c", OK: "#b8bb26", Warn: "#fe8019", Bad: "#fb4934", Tool: "#d3869b",
		Muted: "#928374", Think: "#83a598", Border: "#504945"},
	{Name: "gruvbox-light", Background: "#fbf1c7", Foreground: "#3c3836",
		Accent: "#b57614", Cyan: "#427b58", OK: "#79740e", Warn: "#af3a03", Bad: "#9d0006", Tool: "#8f3f71",
		Muted: "#928374", Think: "#076678", Border: "#d5c4a1"},
	{Name: "catppuccin-mocha", Dark: true, Background: "#1e1e2e", Foreground: "#cdd6f4",
		Accent: "#cba6f7", Cyan: "#89dceb", OK: "#a6e3a1", Warn: "#f9e2af", Bad: "#f38ba8", Tool: "#f5c2e7",
		Muted: "#7f849c", Think: "#b4befe", Border: "#45475a"},
	{Name: "catppuccin-latte", Background: "#eff1f5", Foreground: "#4c4f69",
		Accent: "#8839ef", Cyan: "#04a5e5", OK: "#40a02b", Warn: "#df8e1d", Bad: "#d20f39", Tool: "#ea76cb",
		Muted: "#8c8fa1", Think: "#7287fd", Border: "#bcc0cc"},
	{Name: "tokyo-night", Dark: true, Background: "#1a1b26", Foreground: "#c0caf5",
		Accent: "#7aa2f7", Cyan: "#7dcfff", OK: "#9ece6a", Warn: "#e0af68", Bad: "#f7768e", Tool: "#bb9af7",
		Muted: "#565f89", Think: "#9d7cd8", Border: "#414868"},
	{Name: "one-dark", Dark: true, Background: "#282c34", Foreground: "#abb2bf",
		Accent: "#61afef", Cyan: "#56b6c2", OK: "#98c379", Warn: "#e5c07b", Bad: "#e06c75", Tool: "#c678dd",
		Muted: "#5c6370", Think: "#828997", Border: "#4b5263"},
	{Name: "monokai", Dark: true, Background: "#272822", Foreground: "#f8f8f2",
		Accent: "#ae81ff", Cyan: "#66d9ef", OK: "#a6e22e", Warn: "#fd971f", Bad: "#f92672", Tool: "#f92672",
		Muted: "#75715e", Think: "#ae81ff", Border: "#49483e"},
}

// Names are the values the theme setting takes: auto, then every palette.
func Names() []string {
	names := []string{Auto}
	for _, p := range palettes {
		names = append(names, p.Name)
	}
	return names
}

// Named is the palette called name, with its grounds filled in; false when there is none.
func Named(name string) (Palette, bool) {
	for _, p := range palettes {
		if p.Name == name {
			return p.withGrounds(), true
		}
	}
	return Palette{}, false
}

// For is what auto draws with: ykode's palette that reads on a background of this shade.
func For(dark bool) Palette {
	if dark {
		p, _ := Named("ykode-dark")
		return p
	}
	p, _ := Named("ykode-light")
	return p
}

// Check accepts auto or a palette's name, and names the choices otherwise.
func Check(name string) error {
	if name == Auto {
		return nil
	}
	if _, ok := Named(name); !ok {
		return fmt.Errorf("theme %q: want one of %v", name, Names())
	}
	return nil
}

// the share of OK or Bad a change's row and its changed words take over the background
const (
	groundMix = 0.22
	wordMix   = 0.42
)

// withGrounds mixes the change grounds a theme left out from its background, so a diff reads on it
func (p Palette) withGrounds() Palette {
	if p.Background == "" {
		return p // nothing to mix with: a theme on the terminal's own background names them or goes without
	}
	fill := func(ground *string, over string, share float64) {
		if *ground == "" {
			*ground = mix(p.Background, over, share)
		}
	}
	fill(&p.AddGround, p.OK, groundMix)
	fill(&p.DelGround, p.Bad, groundMix)
	fill(&p.AddWordGround, p.OK, wordMix)
	fill(&p.DelWordGround, p.Bad, wordMix)
	return p
}

// mix is the colour share of the way from one hex colour to another; "" when either is not hex
func mix(from, to string, share float64) string {
	a, okA := rgb(from)
	b, okB := rgb(to)
	if !okA || !okB {
		return ""
	}
	var out [3]int
	for i := range out {
		out[i] = int(float64(a[i]) + (float64(b[i])-float64(a[i]))*share + 0.5)
	}
	return fmt.Sprintf("#%02x%02x%02x", out[0], out[1], out[2])
}

func rgb(hex string) ([3]int, bool) {
	if len(hex) != 7 || hex[0] != '#' {
		return [3]int{}, false
	}
	v, err := strconv.ParseUint(hex[1:], 16, 32)
	if err != nil {
		return [3]int{}, false
	}
	return [3]int{int(v >> 16 & 0xff), int(v >> 8 & 0xff), int(v & 0xff)}, true
}
