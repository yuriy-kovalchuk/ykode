// Package images makes a file's bytes into something a model that reads images can be sent: a format a
// vision encoder decodes, and small enough for the request it travels in. A picture too big for the
// window is worse than a smaller one: the server refuses the whole conversation, not the picture.
package images

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"

	_ "image/gif" // a gif decoder: the encoders below are imported for their own use

	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

// The ceiling a picture may reach once it is encoded. A vision encoder downsamples far before this,
// so keeping under it costs nothing the model would have wanted back.
const (
	MimePng  = "image/png"
	MimeJpeg = "image/jpeg"
	MimeGif  = "image/gif"
	MimeWebp = "image/webp"

	maxSide       = 2000      // the longest edge a model is sent
	maxEncoded    = 4 << 20   // base64 bytes, with room left for the rest of the request
	firstQuality  = 80        // where the jpeg quality ladder starts
	shrinkTo      = 3.0 / 4.0 // what one more step of the ladder takes off each edge
	maxShrinkings = 4         // steps before the picture is given up on
)

// Kind is the mime type of the image these first bytes start with, or "" when they start no image this
// package can prepare. The bytes after them are never looked at, so this is safe on a buffer that was
// only read as far as the signatures.
func Kind(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}):
		return MimePng
	case len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff:
		return MimeJpeg // 0xff 0xf7 is JPEG 2000, which no server here takes
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return MimeGif
	case len(data) >= 12 && bytes.HasPrefix(data, []byte("RIFF")) && string(data[8:12]) == "WEBP":
		return MimeWebp
	}
	return ""
}

// Prepared is a picture as a request can carry it.
type Prepared struct {
	MIME string
	Data string // base64, with no data URL around it
	Note string // what was done to it, so a model can map what it sees back to the file
}

// Prepare decodes a picture, brings it inside the ceiling, and encodes it as whichever of png and jpeg
// is smaller. It fails when the bytes are not a picture, or when no step of the ladder gets them under
// the ceiling: the caller then says so in words instead of sending a request the server refuses.
func Prepare(data []byte, mime string) (Prepared, error) {
	src, err := decode(data, mime)
	if err != nil {
		return Prepared{}, fmt.Errorf("%s image could not be decoded: %w", mime, err)
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	if w <= 0 || h <= 0 {
		return Prepared{}, fmt.Errorf("%s image holds no pixels", mime)
	}
	scale := 1.0
	if w > maxSide || h > maxSide {
		scale = float64(maxSide) / float64(max(w, h))
	}
	pic := src
	for step := 0; ; step++ {
		if scale < 1 {
			pic = scaleDown(src, scale)
		}
		if best, ok := smallestUnder(pic); ok {
			return describe(mime, best, w, h, scale), nil
		}
		if step == maxShrinkings {
			return Prepared{}, fmt.Errorf("a %dx%d %s does not fit the %d KB an image may cost", w, h, mime, maxEncoded>>10)
		}
		scale *= shrinkTo
	}
}

// decode reads a picture. webp keeps to itself: its decoder is not registered globally, because a jpeg
// and a webp picture can carry the same bytes.
func decode(data []byte, mime string) (image.Image, error) {
	if mime == MimeWebp {
		return webp.Decode(bytes.NewReader(data))
	}
	pic, _, err := image.Decode(bytes.NewReader(data))
	return pic, err
}

// smallestUnder is the cheapest of png and jpeg that fits, and whether either did.
func smallestUnder(pic image.Image) (Prepared, bool) {
	var best Prepared
	pngBytes, err := encodePNG(pic)
	if err == nil && len(pngBytes) <= maxEncoded {
		best = Prepared{MIME: MimePng, Data: encoded(pngBytes)}
	}
	for _, quality := range []int{firstQuality, firstQuality / 2, firstQuality / 4} {
		jpegBytes, err := encodeJPEG(pic, quality)
		if err != nil || len(jpegBytes) > maxEncoded {
			continue
		}
		if best.Data == "" || len(jpegBytes) < len(best.Data) {
			best = Prepared{MIME: MimeJpeg, Data: encoded(jpegBytes)}
		}
	}
	return best, best.Data != ""
}

// describe is what went out, with a note when the picture the model sees is not the file as stored: a
// model told where it stands then reads a screenshot without guessing at the scale.
func describe(from string, best Prepared, w int, h int, scale float64) Prepared {
	if scale == 1 && best.MIME == from {
		return Prepared{MIME: best.MIME, Data: best.Data}
	}
	note := fmt.Sprintf("[Image: %s", best.MIME)
	if best.MIME != from {
		note += fmt.Sprintf(", re-encoded from %s", from)
	}
	if scale < 1 {
		shown := int(float64(w)*scale + 0.5)
		tall := int(float64(h)*scale + 0.5)
		note += fmt.Sprintf("; %dx%d in the file, sent at %dx%d. Multiply coordinates by %.2f to map to the file.",
			w, h, shown, tall, 1/scale)
	}
	return Prepared{MIME: best.MIME, Data: best.Data, Note: note + "]"}
}

func scaleDown(src image.Image, scale float64) image.Image {
	b := src.Bounds()
	w, h := max(int(float64(b.Dx())*scale), 1), max(int(float64(b.Dy())*scale), 1)
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	return dst
}

func encodePNG(pic image.Image) ([]byte, error) {
	var out bytes.Buffer
	err := png.Encode(&out, pic)
	return out.Bytes(), err
}

func encodeJPEG(pic image.Image, quality int) ([]byte, error) {
	var out bytes.Buffer
	err := jpeg.Encode(&out, pic, &jpeg.Options{Quality: quality})
	return out.Bytes(), err
}

func encoded(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
