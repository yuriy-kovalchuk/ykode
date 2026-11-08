package tui

import (
	"fmt"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/home"
)

// settings answers /settings: what this run reads, one knob explained, or a change written to
// settings.yaml. A change lands in the file and is read by the next run.
func (m *model) settings(line string) string {
	_, rest, _ := strings.Cut(line, " ")
	key, value, hasValue := strings.Cut(strings.TrimSpace(rest), " ")
	switch {
	case key == "":
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

// settingsChange writes one knob and says what moved. The value is checked by the settings package
// before the file is touched, so a bad one leaves the file as it was.
func (m *model) settingsChange(key, value string) string {
	before, known := find(m.cfg.Catalogue(), key)
	path, err := config.Set(key, value)
	if err != nil {
		return "not changed: " + err.Error()
	}
	if !known {
		return fmt.Sprintf("%s set to %s · %s · the next run reads it", key, value, shortenHome(path))
	}
	return fmt.Sprintf("%s: %s → %s\n  %s · the next run reads it",
		key, orUnset(before.Value), orUnset(value), shortenHome(path))
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
