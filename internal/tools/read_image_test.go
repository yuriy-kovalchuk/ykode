package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/images"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// pngOf is a w x h picture as a file holds it: the tests need a picture whose size they chose, and a
// file made of the right bytes is the only honest way to hand one over.
func pngOf(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 90, A: 255})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func readPicture(t *testing.T, files map[string]string, path string) Result {
	t.Helper()
	root, dir := treeFixture(t, files)
	res, err := NewReadFile(root, dir).RunWithAttachments(context.Background(),
		json.RawMessage(`{"path":"`+path+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestAPictureReadFromAFileComesBackAsAnAttachment(t *testing.T) {
	res := readPicture(t, map[string]string{"shot.png": pngOf(t, 8, 6)}, "shot.png")

	if len(res.Attachments) != 1 {
		t.Fatalf("the answer carries %d attachments, want the picture", len(res.Attachments))
	}
	got := res.Attachments[0]
	if got.Modality != llm.ModalityImage || got.MIME != images.MimePng {
		t.Errorf("the attachment is %v", got)
	}
	raw, err := base64.StdEncoding.DecodeString(got.Data)
	if err != nil {
		t.Fatalf("the picture is not base64: %v", err)
	}
	cfg, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("the picture that came back does not decode: %v", err)
	}
	if b := cfg.Bounds(); b.Dx() != 8 || b.Dy() != 6 {
		t.Errorf("the picture is %dx%d, want 8x6", b.Dx(), b.Dy())
	}
	if !strings.Contains(res.Text, "shot.png") || !strings.Contains(res.Text, "image/png") {
		t.Errorf("the text does not say which file and kind it read: %q", res.Text)
	}
}

// a model reading a screenshot says where a thing it saw sits: without the scale it can only guess
func TestAHugePictureComesBackSmallerAndSaysByHowMuch(t *testing.T) {
	res := readPicture(t, map[string]string{"big.png": pngOf(t, 2600, 40)}, "big.png")

	if len(res.Attachments) != 1 {
		t.Fatalf("the answer carries %d attachments", len(res.Attachments))
	}
	if !strings.Contains(res.Text, "2600x40") || !strings.Contains(res.Text, "Multiply coordinates by") {
		t.Errorf("the text does not give the scale back: %q", res.Text)
	}
}

func TestAFileThatIsNeitherTextNorAPictureSaysSo(t *testing.T) {
	root, dir := treeFixture(t, map[string]string{"bundle.zip": "PK\x03\x04\x00\x00 binary"})
	defer root.Close()

	_, err := NewReadFile(root, dir).RunWithAttachments(context.Background(), json.RawMessage(`{"path":"bundle.zip"}`))
	if err == nil || !strings.Contains(err.Error(), "binary") || !strings.Contains(err.Error(), "exec_command") {
		t.Fatalf("err = %v, want it to name the kind and say what to do instead", err)
	}
}

// a picture too big for ykode to take is refused by size, not by kind: the message has to say which,
// or the model reads the same file again hoping a different window helps
func TestAPictureTooBigForYkodeIsNamedAsTooBig(t *testing.T) {
	root, dir := treeFixture(t, map[string]string{"huge.png": strings.Repeat("\x00", maxPictureFileBytes+1)})
	defer root.Close()

	_, err := NewReadFile(root, dir).RunWithAttachments(context.Background(), json.RawMessage(`{"path":"huge.png"}`))
	if err == nil || !strings.Contains(err.Error(), "picture ykode reads") {
		t.Fatalf("err = %v, want the size ceiling for a picture", err)
	}
}
