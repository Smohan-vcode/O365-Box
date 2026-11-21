package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// wrap breaks text into lines of at most width cells, keeping paragraphs.
func wrap(s string, width int) string { return wrapHang(s, width, "", "") }

// wrapIndent indents every line.
func wrapIndent(s string, width int, indent string) string { return wrapHang(s, width, indent, indent) }

// wrapHang wraps with one prefix for a paragraph's first line and another
// for its continuation lines.
func wrapHang(s string, width int, first, rest string) string {
	if width < 10 {
		width = 10
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := first + words[0]
		for _, w := range words[1:] {
			if lipgloss.Width(line)+1+lipgloss.Width(w) > width {
				out = append(out, line)
				line = rest + w
				continue
			}
			line += " " + w
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// fit cuts every line to width (ANSI-aware) and pads or cuts the block to
// height lines.
func fit(s string, width, height int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if height >= 0 && len(lines) > height {
		lines = lines[:height]
	}
	for i, l := range lines {
		if lipgloss.Width(l) > width {
			lines[i] = ansi.Truncate(l, width, "…")
		}
	}
	for height >= 0 && len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
