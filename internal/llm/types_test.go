package llm

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

// sentMessages is the conversation as the server would read it, in the shape it arrived in.
func sentMessages(t *testing.T, req ChatRequest) []map[string]any {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var sent struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatal(err)
	}
	return sent.Messages
}

// what a model takes as input is the client's own business, and the server is not told about it
func TestWhatAModelTakesIsNotSentToIt(t *testing.T) {
	body, err := json.Marshal(ChatRequest{Model: "m", Modalities: []string{ModalityImage},
		Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "modalities") {
		t.Errorf("the client's own capability note went to the server: %s", body)
	}
}

// only an attachment makes content stop being a string: a server compares a request token by token
func TestTextOnlyConversationKeepsTheShapeServersAlreadyGet(t *testing.T) {
	said := sentMessages(t, ChatRequest{Messages: []Message{
		{Role: "system", Content: "the instructions"},
		{Role: "user", Content: "read a.txt"},
		{Role: "tool", Name: "read_file", ToolCallID: "c1", CallID: "r1", Content: "five lines"},
	}})
	if len(said) != 3 {
		t.Fatalf("the request carried %d messages", len(said))
	}
	for i, m := range said {
		if _, isText := m["content"].(string); !isText {
			t.Errorf("message %d is not the plain string a server expects: %v", i, m["content"])
		}
		if _, ok := m["attachments"]; ok {
			t.Errorf("message %d carries ykode's own attachment field: %v", i, m)
		}
	}
}

func TestAnImageTravelsAsAContentPartBesideTheText(t *testing.T) {
	said := sentMessages(t, ChatRequest{Modalities: []string{ModalityImage}, Messages: []Message{
		{Role: "user", Content: "what is in this?", Attachments: []Attachment{
			{Modality: ModalityImage, MIME: "image/png", Data: "aGVsbG8="},
		}},
	}})
	parts, ok := said[0]["content"].([]any)
	if !ok || len(parts) != 2 {
		t.Fatalf("content = %v, want the text and one image part", said[0]["content"])
	}
	text := parts[0].(map[string]any)
	if text["type"] != partText || text["text"] != "what is in this?" {
		t.Errorf("the ask is not the first part: %v", text)
	}
	image := parts[1].(map[string]any)
	url := image["image_url"].(map[string]any)["url"]
	if image["type"] != partImage || url != "data:image/png;base64,aGVsbG8=" {
		t.Errorf("the image went out as %v", image)
	}
}

func TestAnImageWithoutAnyTextSendsJustTheImage(t *testing.T) {
	said := sentMessages(t, ChatRequest{Modalities: []string{ModalityImage}, Messages: []Message{
		{Role: "user", Attachments: []Attachment{{Modality: ModalityImage, MIME: "image/png", Data: "x"}}},
	}})
	parts, ok := said[0]["content"].([]any)
	if !ok || len(parts) != 1 {
		t.Fatalf("content = %v, want the image on its own", said[0]["content"])
	}
	if _, ok := parts[0].(map[string]any)["image_url"]; !ok {
		t.Errorf("the one part is not an image: %v", parts[0])
	}
}

// a model with no vision encoder gets text only; the bytes stay in history for a model that takes them
func TestAModelThatTakesNoImagesIsSentOnlyText(t *testing.T) {
	req := ChatRequest{Messages: []Message{
		{Role: "user", Content: "what is in this?", Attachments: []Attachment{
			{Modality: ModalityImage, MIME: "image/png", Data: "aGVsbG8="},
		}},
	}}
	said := sentMessages(t, req)
	if content, isText := said[0]["content"].(string); !isText || content != "what is in this?" {
		t.Errorf("content = %v, want the ask as text", said[0]["content"])
	}
	if len(req.Messages[0].Attachments) != 1 {
		t.Error("an attachment that could not travel was taken out of the conversation")
	}
}

func TestAModalityThisClientCannotWriteIsNotSent(t *testing.T) {
	said := sentMessages(t, ChatRequest{Modalities: []string{"audio"}, Messages: []Message{
		{Role: "user", Content: "listen", Attachments: []Attachment{
			{Modality: ModalityImage, MIME: "image/png", Data: "x"},
		}},
	}})
	if _, isText := said[0]["content"].(string); !isText {
		t.Errorf("content = %v, want the ask as text", said[0]["content"])
	}
}

// a tool reply answers its call, so what it read follows the run as one message of its own
func TestWhatAToolReadTravelsAfterTheRunOfReplies(t *testing.T) {
	said := sentMessages(t, ChatRequest{Modalities: []string{ModalityImage}, Messages: []Message{
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1"}, {ID: "c2"}}},
		{Role: "tool", Name: "read_file", ToolCallID: "c1", CallID: "r1", Content: "Read image file [image/png]",
			Attachments: []Attachment{{Modality: ModalityImage, MIME: "image/png", Data: "AAA="}}},
		{Role: "tool", Name: "exec_command", ToolCallID: "c2", CallID: "r2", Content: "the command output"},
		{Role: "assistant", Content: "I can see it"},
	}})
	if len(said) != 5 {
		t.Fatalf("the request carried %d messages, want the four plus one carrying the image", len(said))
	}
	for i := 1; i <= 2; i++ {
		if _, isText := said[i]["content"].(string); !isText {
			t.Errorf("tool reply %d carries parts: %v", i, said[i]["content"])
		}
	}
	last := said[3]
	if last["role"] != "user" {
		t.Fatalf("the image travelled in a %v message", last["role"])
	}
	if _, ok := last["call_id"]; ok {
		t.Errorf("the message carrying the images has ykode's own id on it: %v", last)
	}
	parts := last["content"].([]any)
	if len(parts) != 2 {
		t.Fatalf("it carries %d parts, want the note and the image", len(parts))
	}
	if parts[0].(map[string]any)["text"] != imagesNote {
		t.Errorf("the message says %v", parts[0])
	}
	if _, ok := parts[1].(map[string]any)["image_url"]; !ok {
		t.Errorf("the image is not in it: %v", parts[1])
	}
	if said[4]["content"] != "I can see it" {
		t.Errorf("what came after the run moved: %v", said[4]["content"])
	}
}

func TestARunThatReadNothingIsNotGivenAnEmptyMessage(t *testing.T) {
	said := sentMessages(t, ChatRequest{Modalities: []string{ModalityImage}, Messages: []Message{
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1"}}},
		{Role: "tool", Name: "exec_command", ToolCallID: "c1", CallID: "r1", Content: "(no output)"},
	}})
	if len(said) != 2 {
		t.Fatalf("the request carried %d messages: %v", len(said), said)
	}
}

// a preview cut inside a character backs off to the last whole one, so the quote stays valid UTF-8
func TestAPreviewNeverSplitsACharacter(t *testing.T) {
	got := preview(strings.Repeat("a", 119) + strings.Repeat("é", 10))
	if !utf8.ValidString(got) || got != strings.Repeat("a", 119)+"..." {
		t.Errorf("preview = %q", got)
	}
}
