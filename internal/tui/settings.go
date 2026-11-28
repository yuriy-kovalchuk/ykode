package tui

import (
	"cmp"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/home"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// settings answers /settings list and /settings KEY; a value is changed only on the screen a bare /settings opens
func (m *model) settings(line string) string {
	_, rest, _ := strings.Cut(line, " ")
	key, _, hasValue := strings.Cut(strings.TrimSpace(rest), " ")
	switch {
	case hasValue:
		return "not changed: a setting is changed on the screen a bare /settings opens"
	case key == "" || key == "list":
		return m.settingsTable()
	default:
		return m.settingsOne(key)
	}
}

// settingsTable is every knob the file can set; the unchosen ones read as their default
func (m *model) settingsTable() string {
	list := m.cfg.Catalogue()
	width := 0
	for _, s := range list {
		width = max(width, len(s.Key))
	}
	rows := []string{fmt.Sprintf("what this run reads · change one on the /settings screen · the file is %s",
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

// settingsOne explains one knob: its purpose, its value, its default, and how to change it
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
	return strings.Join(append(rows, "  change it: on the screen a bare /settings opens"), "\n")
}

// noLocalFile warns that a project without its own file is changing everyone's file
const noLocalFile = "no .ykode/settings.yaml in this project: this changes the global settings"

// createLocal gives the project its own settings file, from this run, and reloads the settings
func (m *model) createLocal() {
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

// modalMsg gives the overlay its message; keys go to the modal, the rest to the session below
func (m *model) modalMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg.(type) {
	case tea.KeyMsg, tea.PasteMsg, probeDoneMsg:
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

// applyEndpoint saves the choice to this project's file and switches the running session to it
func (m *model) applyEndpoint(res SetupResult) {
	path, local, err := config.SaveEndpoint(m.cfg.Root, res.BaseURL, res.Model)
	if err != nil {
		m.err = err.Error()
		return
	}
	m.cfg.BaseURL, m.cfg.Model, m.cfg.EndpointSet = res.BaseURL, res.Model, true
	client := llm.NewClient(res.BaseURL)
	client.Session, client.Idle = m.cfg.Session, m.cfg.StreamIdle
	if !m.agent.SetEndpoint(client, llm.ModelInfo{ID: res.Model, CtxSize: res.CtxSize, Vision: res.Vision}, cmp.Or(m.cfg.CtxSize, res.CtxSize)) {
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
