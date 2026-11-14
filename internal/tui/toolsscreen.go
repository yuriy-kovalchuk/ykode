package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// toolsScreen is /tools: what the model may call, and what each description costs every request. The
// command prints a column of names, which answers "how many" but not "which one reads files" or "which
// one is costing the most"; here the list narrows and a row says its share.
type toolsScreen struct {
	chooser
	specs []agent.Spec
	total int
}

func (m *model) openTools() {
	specs := m.agent.Specs()
	s := &toolsScreen{chooser: newChooser("tools", "the model may call these",
		"type to narrow · ↑/↓ moves · esc closes"), specs: specs}
	for _, spec := range specs {
		s.total += spec.Bytes
	}
	s.load(s.toolRows())
	m.picker = s
}

func (s *toolsScreen) toolRows() []listRow {
	rows := make([]listRow, len(s.specs))
	for i, spec := range s.specs {
		rows[i] = listRow{
			id:    spec.Name,
			text:  spec.Name,
			hay:   spec.Name,
			cells: []listCell{{text: padTo(fmt.Sprintf("%d bytes", spec.Bytes), 12)}},
		}
	}
	return rows
}

func (s *toolsScreen) toolDetail(id string) []string {
	for _, spec := range s.specs {
		if spec.Name != id {
			continue
		}
		share := 0
		if s.total > 0 {
			share = spec.Bytes * 100 / s.total
		}
		return []string{
			spec.Name,
			fmt.Sprintf("%d bytes in every request, %d%% of the %d the tools cost to describe", spec.Bytes, share, s.total),
		}
	}
	return nil
}

// nothingToChoose says what this list is: read it, or narrow it. Enter is not asked to do anything.
func (s *toolsScreen) pick(id string) (tea.Cmd, overlayNext) {
	if id == "" {
		return nil, stayOpen
	}
	s.note = "nothing to choose here: this list is to read"
	return nil, stayOpen
}

func (s *toolsScreen) keys(msg tea.Msg) (tea.Cmd, overlayNext, bool) {
	return s.chooser.keys(msg, s.pick)
}

func (s *toolsScreen) view(width, height int) string {
	return s.chooser.view(width, height, s.toolDetail)
}
