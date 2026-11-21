package agent

import (
	"fmt"
	"strings"
	"testing"
)

// an @ word is a picture only by its ending, and a path keeps the spaces it was quoted or escaped with
func TestAnAskNamesPicturesByTheirEnding(t *testing.T) {
	ask := `what is wrong in @shot.png, and @"My Shot 2.PNG" and @dir/a\ b.jpg? ` +
		`ask @someone, mail me@host.png, see @notes.md`
	got := fmt.Sprint(pictureRefs(ask))
	if want := "[shot.png My Shot 2.PNG dir/a b.jpg]"; got != want {
		t.Errorf("found %s, want %s", got, want)
	}
}

// a picture the ask names travels with it, read from where the user keeps it, project or not
func TestAPictureNamedInTheAskReachesTheModel(t *testing.T) {
	project, elsewhere := t.TempDir(), t.TempDir()
	pictureFile(t, project, "shot.png")
	pictureFile(t, elsewhere, "desk top.png")
	stub := &stubServer{scripts: []string{sse("", "a red button", "", nil, "stop")}}
	a, done := newAgentIn(t, stub, Options{Vision: true, Root: project}, &captureSink{}, project)
	defer done()
	if err := a.Ask(t.Context(), `compare @shot.png with @"`+elsewhere+`/desk top.png"`); err != nil {
		t.Fatal(err)
	}
	urls := pictureURLs(stub.lastRequest(t))
	if len(urls) != 2 || !strings.HasPrefix(urls[0], "data:image/png;base64,") {
		t.Errorf("the request carried %d pictures, want both the ask named", len(urls))
	}
}

// a picture that cannot be read is said, and the ask still goes, since its words still mean something
func TestAPictureThatCannotBeReadIsSaidAndTheAskStillGoes(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "fine", "", nil, "stop")}}
	sink := &captureSink{}
	a, done := newAgent(t, stub, Options{Vision: true}, sink)
	defer done()
	if err := a.Ask(t.Context(), "look at @gone.png"); err != nil {
		t.Fatal(err)
	}
	if !sink.noticed("gone.png was not attached") {
		t.Error("a missing picture went unsaid")
	}
	if len(pictureURLs(stub.lastRequest(t))) != 0 {
		t.Error("a picture that was never read went out")
	}
}

// a model that reads no pictures is told nothing it cannot take, and the user hears why
func TestAPictureForAModelThatReadsNoneStaysOutAndIsSaid(t *testing.T) {
	dir := t.TempDir()
	pictureFile(t, dir, "shot.png")
	stub := &stubServer{scripts: []string{sse("", "fine", "", nil, "stop")}}
	sink := &captureSink{}
	a, done := newAgentIn(t, stub, Options{Root: dir}, sink, dir)
	defer done()
	if err := a.Ask(t.Context(), "look at @shot.png"); err != nil {
		t.Fatal(err)
	}
	if len(pictureURLs(stub.lastRequest(t))) != 0 || !sink.noticed("reads no pictures") {
		t.Error("a text-only model was sent the picture, or the user was not told it was left out")
	}
}
