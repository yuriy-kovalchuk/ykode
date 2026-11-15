package tui

import (
	"cmp"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/home"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// settings answers /settings: what this run reads, one knob explained, or a change written to
// settings.yaml. A change lands in the file and is read by the next run.
func (m *model) settings(line string) string {
	_, rest, _ := strings.Cut(line, " ")
	key, value, hasValue := strings.Cut(strings.TrimSpace(rest), " ")
	switch {
	case key == "" || key == "list":
		return m.settingsTable()
	case !hasValue:
		return m.settingsOne(key)
	default:
		return m.settingsChange(key, value)
	}
}

// settingsTable is every knob the file can set. The ones nobody chose read as their default and say
// nothing more, so what someone changed is what stands out.
func (m *model) settingsTable() string {
	list := m.cfg.Catalogue()
	width := 0
	for _, s := range list {
		width = max(width, len(s.Key))
	}
	rows := []string{fmt.Sprintf("what this run reads · change one with: /settings KEY VALUE · the file is %s",
		shortenHome(home.Settings()))}
	for _, s := range list {
		row := fmt.Sprintf("  %-*s  %s", width, s.Key, orUnset(s.Value))
		if s.From != config.FromDefault {
			row += fmt.Sprintf("\x1b[2m\twas %s · %s\x1b[0m", orUnset(s.Default), s.From)
		}
		rows = append(rows, row)
	}
	return strings.Join(rows, "\n")
}

// settingsOne explains a single knob: what it is for, what it is now, what it would be, and how to
// change it.
func (m *model) settingsOne(key string) string {
	s, ok := find(m.cfg.Catalogue(), key)
	if !ok {
		return m.noSuchSetting(key)
	}
	rows := []string{s.Key, "  " + s.Note,
		fmt.Sprintf("  now %s · default %s · from %s", orUnset(s.Value), orUnset(s.Default), s.From)}
	if len(s.Options) > 0 {
		rows = append(rows, "  choices: "+strings.Join(s.Options, ", "))
	}
	return strings.Join(append(rows, fmt.Sprintf("  change it: /settings %s VALUE", s.Key)), "\n")
}

// settingsChange writes one knob and says what moved, naming the file that took it. The value is
// checked by the settings package before the file is touched, so a bad one leaves the file as it was.
func (m *model) settingsChange(key, value string) string {
	before, known := find(m.cfg.Catalogue(), key)
	path, local, err := config.Set(m.cfg.Root, key, value)
	if err != nil {
		return "not changed: " + err.Error()
	}
	if !known {
		return savedTo(key, path, local) + " · the next run reads it"
	}
	line := fmt.Sprintf("%s: %s → %s\n  %s · the next run reads it",
		key, orUnset(before.Value), orUnset(value), savedTo(key, path, local))
	if !local {
		line = noLocalFile + "\n  " + line
	}
	return line
}

// noLocalFile warns that a project without its own settings file is having the file every project
// reads changed.
const noLocalFile = "no .ykode/settings.yaml in this project: this changes the global settings"

// createLocal gives the project a settings file of its own, starting from the server and model this
// run is using, and reads the settings again so the screen shows where each value comes from.
func (m *model) createLocal() {
	if m.running {
		m.log.notice("that waits for the turn to finish: press esc to stop this one first")
		return
	}
	path, err := config.CreateLocal(m.cfg.Root, m.cfg.BaseURL, m.agent.Model())
	if err != nil {
		m.err = err.Error()
		return
	}
	if fresh, err := m.cfg.Reload(); err == nil {
		m.cfg = fresh
	}
	m.log.notice("created " + shortenHome(path) + ": /settings now changes this project only. " +
		"Add .ykode/ to the project's .gitignore if it holds a key.")
}

// savedTo names the file a change landed in: the project's own, or the global one by its full path.
func savedTo(key, path string, local bool) string {
	if local {
		return key + " saved to .ykode/settings.yaml (this project)"
	}
	return key + " saved globally to " + shortenHome(path)
}

// noSuchSetting names the key that was asked for and lists the ones there are.
func (m *model) noSuchSetting(key string) string {
	names := make([]string, 0, 32)
	for _, s := range m.cfg.Catalogue() {
		names = append(names, s.Key)
	}
	return fmt.Sprintf("no setting called %s\n  what there is: %s", key, strings.Join(names, ", "))
}

func find(list []config.Setting, key string) (config.Setting, bool) {
	for _, s := range list {
		if s.Key == key {
			return s, true
		}
	}
	return config.Setting{}, false
}

func orUnset(value string) string {
	if value == "" {
		return "unset"
	}
	return value
}

// modalMsg gives the screen over the session its message. It reports whether the modal took it; a key
// always goes to the modal, and so does what its own probe answered, while everything else is the
// session's, which keeps running underneath.
func (m *model) modalMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg.(type) {
	case tea.KeyMsg, probeDoneMsg:
	default:
		return nil, false
	}
	_, cmd := m.modal.Update(msg)
	if m.modal.closed {
		chosen, res := m.modal.done, m.modal.result
		m.modal, cmd = nil, nil // the modal's own quit is not the session's
		if chosen {
			m.applyEndpoint(res)
		}
		m.refresh()
	}
	return cmd, true
}

// applyEndpoint saves the choice to the settings file this project uses and switches the running
// session to it, so the next request goes to the new server with the new model and its window.
func (m *model) applyEndpoint(res SetupResult) {
	path, local, err := config.SaveEndpoint(m.cfg.Root, res.BaseURL, res.APIKey, res.Model)
	if err != nil {
		m.err = err.Error()
		return
	}
	if res.APIKey != "" {
		m.cfg.APIKey = res.APIKey
	}
	m.cfg.BaseURL, m.cfg.Model, m.cfg.EndpointSet = res.BaseURL, res.Model, true
	client := llm.NewClient(res.BaseURL, m.cfg.APIKey)
	client.Session, client.Idle = m.cfg.Session, m.cfg.StreamIdle
	if !m.agent.SetEndpoint(client, res.Model, cmp.Or(m.cfg.CtxSize, res.CtxSize)) {
		// the choice is saved either way: only switching this run over had to wait
		m.log.notice("saved; this run keeps its current model until the turn finishes")
		m.refresh()
		return
	}
	where := shortenHome(path)
	if !local {
		where = "globally in " + where + ": " + noLocalFile
	}
	m.log.notice(fmt.Sprintf("now using %s at %s · saved to %s", res.Model, res.BaseURL, where))
}

// mouseCmd tells the terminal about a mouse setting that changed on the settings screen.
func (m *model) mouseCmd() tea.Cmd {
	if !m.wantMouse {
		return nil
	}
	m.wantMouse = false
	if m.mouseOn {
		return tea.EnableMouseCellMotion
	}
	return tea.DisableMouse
}
