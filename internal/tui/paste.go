package tui

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/yuriy-kovalchuk/ykode/internal/size"
)

// a paste past either mark goes into the box as one token: more than the box shows, or slow to redraw
const (
	pasteLines = maxInputLines
	pasteBytes = 1000
)

// pastes holds the long texts pasted this session; token #n is texts[n-1], kept so history can resend it
type pastes struct {
	texts []string
}

// pasteToken is how a held paste reads in the box and on the ask's card.
var pasteToken = regexp.MustCompile(`\[Pasted text #(\d+)[^\]]*\]`)

// hold keeps a long paste and gives the token that stands for it; a short one is typed as it is
func (p *pastes) hold(text string) (string, bool) {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	lines := strings.Count(strings.TrimRight(text, "\n"), "\n") + 1
	if lines <= pasteLines && len(text) <= pasteBytes {
		return "", false
	}
	p.texts = append(p.texts, text)
	if lines == 1 {
		return fmt.Sprintf("[Pasted text #%d, %s]", len(p.texts), size.Text(len(text))), true
	}
	return fmt.Sprintf("[Pasted text #%d +%d lines]", len(p.texts), lines), true
}

// expand puts every held paste back where its token is, so the model reads what was pasted
func (p *pastes) expand(text string) string {
	return pasteToken.ReplaceAllStringFunc(text, func(token string) string {
		n, err := strconv.Atoi(pasteToken.FindStringSubmatch(token)[1])
		if err != nil || n < 1 || n > len(p.texts) {
			return token // a token this session never handed out is text the user typed
		}
		return p.texts[n-1]
	})
}

// pastedText is the text a paste carries: the terminal's own, or the clipboard's from ctrl+v
func pastedText(msg tea.Msg) (string, bool) {
	if paste, ok := msg.(tea.PasteMsg); ok {
		return paste.Content, true
	}
	// textarea.Paste hands its text over as an unexported string type, read here by its kind
	if v := reflect.ValueOf(msg); msg != nil && v.Kind() == reflect.String {
		return v.String(), true
	}
	return "", false
}

// holdPaste puts a long paste in the box as its token; false leaves the paste to the box itself
func (m *model) holdPaste(msg tea.Msg) bool {
	text, ok := pastedText(msg)
	if !ok {
		return false
	}
	token, held := m.pastes.hold(text)
	if !held {
		return false
	}
	m.input.InsertString(token)
	m.trackSuggestions()
	m.fitInput()
	m.layout()
	return true
}

// isPaste is a paste from either road: the terminal's own, or ctrl+v's read of the clipboard
func isPaste(msg tea.Msg) bool {
	switch msg.(type) {
	case tea.PasteMsg, pastedMsg:
		return true
	}
	return false
}
