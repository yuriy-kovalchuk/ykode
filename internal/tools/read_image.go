package tools

import (
	"fmt"
	"net/http"

	"github.com/yuriy-kovalchuk/ykode/internal/images"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// maxPictureFileBytes is the largest file read_file will take as a picture. It is far above what a
// model is finally sent: the ceiling is on the file, so a 4K screenshot can be read and shrunk rather
// than refused for being what screenshots are.
const maxPictureFileBytes = 12 << 20

// asPicture is the part of a read that is not text: the file is brought to a size and format a vision
// encoder takes, and the text that comes with it says which file it was and what was done to it, so a
// model reading a screenshot can say where in the file a thing it sees sits.
func (t *ReadFile) asPicture(h handle, data []byte, mime string) (Result, error) {
	pic, err := images.Prepare(data, mime)
	if err != nil {
		return Result{}, fmt.Errorf("read_file %q: %w", h.shown(), err)
	}
	said := fmt.Sprintf("Read image file %s [%s]", h.shown(), pic.MIME)
	if pic.Note != "" {
		said += "\n" + pic.Note
	}
	return Result{Text: said, Attachments: []llm.Attachment{
		{Modality: llm.ModalityImage, MIME: pic.MIME, Data: pic.Data},
	}}, nil
}

// sniff names a file by what its first bytes are, for a message about a file ykode cannot read. The
// name is the server's, not a guess from the extension: an extension lies about a file far more often
// than its first four bytes do.
func sniff(data []byte) string {
	return http.DetectContentType(data[:min(len(data), 512)])
}
