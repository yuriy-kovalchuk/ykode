package main

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
)

// pictureTasks show the model a picture named in the ask, the way a pasted screenshot reaches it
func pictureTasks() []task {
	return []task{
		{
			name:    "what-is-in-the-picture",
			prompt:  "@shot.png what colour fills this picture? answer with one word",
			fixture: map[string]string{"shot.png": solidPicture(color.RGBA{R: 220, A: 255})},
			vision:  true,
			check: func(_ string, ran *transcript, _ error) (bool, string) {
				if !ran.contains("red") {
					return false, "answer: " + quote(ran.answer)
				}
				return true, "saw the red the picture holds"
			},
		},
	}
}

// solidPicture is a png of one colour, big enough that an encoder cannot round it away
func solidPicture(fill color.Color) string {
	pic := image.NewRGBA(image.Rect(0, 0, 64, 64))
	draw.Draw(pic, pic.Bounds(), &image.Uniform{C: fill}, image.Point{}, draw.Src)
	var out bytes.Buffer
	_ = png.Encode(&out, pic) // an in-memory encode of a picture made here cannot fail
	return out.String()
}
