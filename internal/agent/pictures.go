package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/images"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// maxAskedPictureBytes is the largest file an ask may attach, as read_file bounds the ones it reads
const maxAskedPictureBytes = 12 << 20

// pictureRef is @path, @"a path with spaces" or @path\ with\ escapes, at the start of a word
var pictureRef = regexp.MustCompile(`(?:^|\s)@(?:"([^"]+)"|((?:\\ |\S)+))`)

// pictureKinds are the endings an @ word is read for: anything else is a name, a handle or an email
var pictureKinds = []string{".png", ".jpg", ".jpeg", ".gif", ".webp"}

// pictureRefs are the paths an ask names as pictures, as typed: quoted, escaped, or under ~
func pictureRefs(ask string) []string {
	var paths []string
	for _, match := range pictureRef.FindAllStringSubmatch(ask, -1) {
		path := match[1]
		if path == "" {
			path = strings.ReplaceAll(strings.TrimRight(match[2], ".,;:!?)"), `\ `, " ")
		}
		ending := strings.ToLower(filepath.Ext(path))
		for _, kind := range pictureKinds {
			if ending == kind {
				paths = append(paths, path)
				break
			}
		}
	}
	return paths
}

// picturesIn reads what the ask names; the user typed the path, so it may lie outside the project
func (a *Agent) picturesIn(ask string) []llm.Attachment {
	var attached []llm.Attachment
	for _, ref := range pictureRefs(ask) {
		pic, err := a.readPicture(ref)
		if err != nil {
			a.tell(fmt.Sprintf("%s was not attached: %v", ref, err))
			continue
		}
		attached = append(attached, llm.Attachment{Modality: llm.ModalityImage, MIME: pic.MIME, Data: pic.Data})
	}
	if len(attached) > 0 && !a.opts.Vision {
		// kept with the ask, so a model switched to later that reads pictures still sees them
		a.tell("this model reads no pictures: the attached ones stay out of its requests")
	}
	return attached
}

func (a *Agent) readPicture(ref string) (images.Prepared, error) {
	path := ref
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, rest)
		}
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(a.opts.Root, path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return images.Prepared{}, err
	}
	if info.Size() > maxAskedPictureBytes {
		return images.Prepared{}, fmt.Errorf("it is %.1f MB, and the largest picture ykode reads is %d MB",
			float64(info.Size())/(1<<20), maxAskedPictureBytes>>20)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return images.Prepared{}, err
	}
	mime := images.Kind(data)
	if mime == "" {
		return images.Prepared{}, fmt.Errorf("it is not a png, jpeg, gif or webp picture")
	}
	return images.Prepare(data, mime)
}
