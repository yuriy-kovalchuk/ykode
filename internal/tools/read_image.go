package tools

import (
	"fmt"
	"net/http"

	"github.com/yuriy-kovalchuk/ykode/internal/images"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// maxPictureFileBytes caps the file, not what the model is sent, so a 4K screenshot is shrunk
const maxPictureFileBytes = 12 << 20

// asPicture is the non-text part of a read: the file sized for a vision encoder, plus what it was
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

// sniff names a file by its first bytes, since an extension lies about a file more often
func sniff(data []byte) string {
	return http.DetectContentType(data[:min(len(data), 512)])
}
