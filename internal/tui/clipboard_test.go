package tui

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

var ctrlV = tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl}

// pressPaste presses ctrl+v and hands the screen what the clipboard command came back with
func pressPaste(t *testing.T, m *model) {
	t.Helper()
	_, cmd := m.Update(ctrlV)
	if cmd == nil {
		t.Fatal("ctrl+v asked nothing of the clipboard")
	}
	m.Update(cmd())
}

// a picture on the clipboard is kept under the home folder and named in the box, to be read before it goes
func TestAPastedPictureIsSavedAndNamedInTheBox(t *testing.T) {
	settingsHome(t, "")
	var shot bytes.Buffer
	if err := png.Encode(&shot, image.NewRGBA(image.Rect(0, 0, 3, 3))); err != nil {
		t.Fatal(err)
	}
	m := newTestModel()
	m.clipboard = func(context.Context) ([]byte, error) { return shot.Bytes(), nil }
	m.input.SetValue("what is this ")
	m.input.CursorEnd()
	pressPaste(t, m)
	ref := regexp.MustCompile(`@(\S+/pastes/paste-[0-9.-]+\.png) $`).FindStringSubmatch(m.input.Value())
	if ref == nil {
		t.Fatalf("the box reads %q, want the saved picture named after what was typed", m.input.Value())
	}
	if saved, err := os.ReadFile(ref[1]); err != nil || !bytes.Equal(saved, shot.Bytes()) {
		t.Errorf("the named file does not hold the picture (err %v)", err)
	}
}

// a missing clipboard program is named, so the fix is one install rather than a silent ctrl+v
func TestAPasteThatCannotReadTheClipboardSaysWhy(t *testing.T) {
	settingsHome(t, "")
	m := newTestModel()
	m.clipboard = func(context.Context) ([]byte, error) {
		return nil, errors.New("pasting a picture needs xclip installed")
	}
	pressPaste(t, m)
	if out := stripCodes(m.render()); !strings.Contains(out, "needs xclip installed") {
		t.Errorf("the reason was not shown:\n%s", out)
	}
	if m.input.Value() != "" {
		t.Errorf("a failed paste typed %q", m.input.Value())
	}
}

// macOS answers with the picture as hex inside «data PNGf…», and anything else is no picture
func TestTheMacClipboardsAnswerIsReadAsThePicture(t *testing.T) {
	got, err := macPicture([]byte("«data PNGf89504E470D0A1A0A»\n"))
	if err != nil || !bytes.Equal(got, []byte("\x89PNG\r\n\x1a\n")) {
		t.Errorf("read %q, %v", got, err)
	}
	if _, err := macPicture([]byte("some copied words")); !errors.Is(err, errNoPicture) {
		t.Errorf("text read as a picture: %v", err)
	}
}
