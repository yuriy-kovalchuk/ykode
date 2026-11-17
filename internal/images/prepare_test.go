package images

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"strings"
	"testing"
)

func picture(w, h int, noise bool) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			if noise {
				img.Set(x, y, color.RGBA{uint8(rand.Intn(256)), uint8(rand.Intn(256)), uint8(rand.Intn(256)), 255})
				continue
			}
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 40, A: 255})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		panic(err)
	}
	return out.Bytes()
}

func TestKindNamesOnlyTheImagesThisCanBeAbout(t *testing.T) {
	cases := map[string][]byte{
		MimePng:  picture(4, 4, false),
		MimeJpeg: {0xff, 0xd8, 0xff, 0xe0, 0, 0, 0, 0},
		MimeGif:  append([]byte("GIF89a"), 0, 0, 0),
		MimeWebp: append(append([]byte("RIFF"), 1, 0, 0, 0), []byte("WEBPVP8 ")...),
	}
	for want, data := range cases {
		if got := Kind(data); got != want {
			t.Errorf("%q read as %q", want, got)
		}
	}
	for _, not := range []string{"package main\n", "PK\x03\x04 zip archive", "\x89PNG short"} {
		if got := Kind([]byte(not)); got != "" {
			t.Errorf("%q read as the image %q", not, got)
		}
	}
}

func TestAPictureAlreadyInsideTheLimitsGoesOutAsItCame(t *testing.T) {
	got, err := Prepare(picture(40, 30, false), MimePng)
	if err != nil {
		t.Fatal(err)
	}
	if got.MIME != MimePng || got.Note != "" {
		t.Errorf("nothing was done to it, yet it came back as %+v", got)
	}
	if w, h := size(t, got); w != 40 || h != 30 {
		t.Errorf("it went out %dx%d, want 40x30", w, h)
	}
}

func TestAPictureWiderThanTheLimitIsSentSmallerAndSaysSo(t *testing.T) {
	got, err := Prepare(picture(3000, 600, false), MimePng)
	if err != nil {
		t.Fatal(err)
	}
	w, h := size(t, got)
	if w > maxSide || h > maxSide {
		t.Fatalf("it went out %dx%d, want both edges at most %d", w, h, maxSide)
	}
	if !strings.Contains(got.Note, "3000x600") || !strings.Contains(got.Note, "Multiply coordinates by") {
		t.Errorf("the note does not say what was done: %q", got.Note)
	}
}

// the note is read beside the bytes it describes, so the kind it names first has to be the kind those
// bytes are: a screenshot stored as a png and handed over as a jpeg is a jpeg to whatever reads it
func TestTheNoteNamesTheKindThatTravelsNotOnlyTheKindInTheFile(t *testing.T) {
	got, err := Prepare(picture(3000, 600, true), MimePng)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.Note, "[Image: "+got.MIME) {
		t.Errorf("the note did not start with the kind that travels (%s): %q", got.MIME, got.Note)
	}
	if got.MIME != MimePng && !strings.Contains(got.Note, MimePng) {
		t.Errorf("the note did not say which kind the file was: %q", got.Note)
	}
}

// a picture made of noise compresses badly: the choice between png and jpeg is what keeps it inside
// what one image may cost, and jpeg wins when png cannot
func TestAPictureIsNotSentPastWhatAnImageMayCost(t *testing.T) {
	got, err := Prepare(picture(2000, 2000, true), MimePng)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Data) > maxEncoded {
		t.Errorf("the encoded picture is %d bytes, want at most %d", len(got.Data), maxEncoded)
	}
	if got.MIME != MimeJpeg {
		t.Errorf("it went out as %s; jpeg is what holds a noisy picture", got.MIME)
	}
}

func TestBytesThatAreNotAPictureSaySo(t *testing.T) {
	_, err := Prepare([]byte("this is a paragraph, not a picture"), MimePng)
	if err == nil || !strings.Contains(err.Error(), "decod") {
		t.Errorf("err = %v, want it to say the picture could not be decoded", err)
	}
}

func size(t *testing.T, p Prepared) (int, int) {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(p.Data)
	if err != nil {
		t.Fatalf("the data is not base64: %v", err)
	}
	cfg, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("what went out does not decode: %v", err)
	}
	b := cfg.Bounds()
	return b.Dx(), b.Dy()
}
