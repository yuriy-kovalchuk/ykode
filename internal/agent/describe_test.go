package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// what a person is asked to approve names the file and says how much, not the JSON the model wrote
func TestAConfirmationNamesTheFileNotTheJSON(t *testing.T) {
	body, err := json.Marshal(map[string]string{"path": "web/index.html", "content": strings.Repeat("<p>x</p>\n", 200)})
	if err != nil {
		t.Fatal(err)
	}
	text := describe("write_file", string(body), nil)
	if strings.Contains(text, `"content"`) {
		t.Errorf("the question shows raw JSON: %s", oneLine(text, 120))
	}
	if !strings.Contains(text, "web/index.html") || !strings.Contains(text, "200 lines") {
		t.Errorf("the question does not say what would be written: %q", oneLine(text, 160))
	}
	warning := describe("write_file", string(body), []string{"/etc/hosts"})
	if !strings.Contains(warning, "OUTSIDE the project root: /etc/hosts") || !strings.Contains(warning, "web/index.html") {
		t.Errorf("the warning lost its point: %q", oneLine(warning, 160))
	}
}

// lastDetailer is a read tool that keeps a last result anyway, though such tools overlap
type lastDetailer struct{}

func (*lastDetailer) Name() string                                    { return "read_file" }
func (*lastDetailer) Class() tools.Class                              { return tools.ClassReads }
func (*lastDetailer) Description() string                             { return "" }
func (*lastDetailer) Schema() json.RawMessage                         { return nil }
func (*lastDetailer) NeedsConfirm() bool                              { return false }
func (*lastDetailer) Targets(json.RawMessage) ([]tools.Target, error) { return nil, nil }
func (*lastDetailer) Run(context.Context, json.RawMessage) (string, error) {
	return "", nil
}
func (*lastDetailer) LastDetail() tools.Detail { return tools.Detail{Change: "whose is it?"} }

func TestAnOverlappingToolReportsNoDetail(t *testing.T) {
	if got := detailOf(&lastDetailer{}); got != (tools.Detail{}) {
		t.Errorf("a tool that overlaps others reported %+v", got)
	}
}
