package tui

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/yuriy-kovalchuk/ykode/internal/home"
	"github.com/yuriy-kovalchuk/ykode/internal/images"
)

// clipWait bounds asking the system for the clipboard, which is a program run for each paste
const clipWait = 3 * time.Second

// errNoPicture is a clipboard holding text or nothing: ctrl+v is then an ordinary paste
var errNoPicture = errors.New("no picture on the clipboard")

// pastedMsg is a picture from the clipboard, saved where the ask can name it
type pastedMsg struct {
	path string
	err  error
	text tea.Msg // the box's own paste of the clipboard's text, read when it held no picture
}

// clipboardPicture reads the clipboard as png through the program the system has for it
func clipboardPicture(ctx context.Context) ([]byte, error) {
	switch {
	case runtime.GOOS == "darwin":
		// osascript converts what a screenshot leaves there to png and answers «data PNGf89504E…»
		out, err := exec.CommandContext(ctx, "osascript", "-e", "the clipboard as «class PNGf»").Output()
		if err != nil {
			return nil, errNoPicture
		}
		return macPicture(out)
	case os.Getenv("WAYLAND_DISPLAY") != "":
		return picturePaste(exec.CommandContext(ctx, "wl-paste", "--no-newline", "--type", "image/png"), "wl-paste")
	default:
		return picturePaste(exec.CommandContext(ctx, "xclip", "-selection", "clipboard", "-t", "image/png", "-o"), "xclip")
	}
}

// macPicture turns osascript's «data PNGf…» answer into the picture's bytes
func macPicture(out []byte) ([]byte, error) {
	text := strings.TrimSpace(string(out))
	text, ok := strings.CutPrefix(text, "«data PNGf")
	if !ok {
		return nil, errNoPicture
	}
	return hex.DecodeString(strings.TrimSuffix(text, "»"))
}

// picturePaste runs a Linux clipboard program, naming it when it is not installed
func picturePaste(cmd *exec.Cmd, name string) ([]byte, error) {
	out, err := cmd.Output()
	var missing *exec.Error
	if errors.As(err, &missing) {
		return nil, fmt.Errorf("pasting a picture needs %s installed", name)
	}
	if err != nil || images.Kind(out) == "" {
		return nil, errNoPicture
	}
	return out, nil
}

// pastePicture saves the clipboard's picture under the home folder, or says there is none to save
func (m *model) pastePicture() tea.Cmd {
	read := m.clipboard
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), clipWait)
		defer cancel()
		data, err := read(ctx)
		if err == nil && !bytes.HasPrefix(data, []byte("\x89PNG")) {
			err = errNoPicture
		}
		if errors.Is(err, errNoPicture) {
			return pastedMsg{err: err, text: textarea.Paste()} // read here, off the screen's goroutine
		}
		if err != nil {
			return pastedMsg{err: err}
		}
		dir, err := home.Ensure("pastes")
		if err != nil {
			return pastedMsg{err: err}
		}
		path := filepath.Join(dir, "paste-"+time.Now().Format("20060102-150405.000")+".png")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return pastedMsg{err: err}
		}
		return pastedMsg{path: path}
	}
}

// pasted names a saved picture in the box, where it can be read and taken out before the ask goes
func (m *model) pasted(msg pastedMsg) tea.Cmd {
	switch {
	case errors.Is(msg.err, errNoPicture):
		if m.holdPaste(msg.text) {
			return nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg.text) // the clipboard holds text: ctrl+v pastes it as the box would
		m.fitInput()
		m.layout()
		return cmd
	case msg.err != nil:
		m.log.notice("the picture was not pasted: " + msg.err.Error())
		m.refresh()
		return nil
	}
	ref := "@" + msg.path
	if strings.ContainsRune(msg.path, ' ') {
		ref = `@"` + msg.path + `"`
	}
	m.input.InsertString(ref + " ")
	m.fitInput()
	m.layout()
	return nil
}
