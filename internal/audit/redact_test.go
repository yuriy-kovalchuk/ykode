package audit

import "testing"

// The log records commands and diffs word for word, so anything that reads like a credential has
// to be masked before it reaches the disk - and ordinary talk about keys has to survive intact.
func TestWhatTheAuditLogMasks(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`curl -H "Authorization: Bearer sk-abc123DEF456"`, `curl -H "Authorization: Bearer [masked]"`},
		{`{"api_key": "hunter2"}`, `{"api_key": [masked]}`},
		{`--password hunter2`, `--password [masked]`},
		{`OPENAI_API_KEY=sk-proj-abcdef12345`, `OPENAI_API_KEY=[masked]`},
		{`token: "ghp_16chars_here"`, `token: [masked]`},
		{`SLACK_TOKEN=xoxb-1234-5678-abcd`, `SLACK_TOKEN=[masked]`},
		{`aws key id AKIA1234567890ABCDEF`, `aws key id [masked]`},
		{"-----BEGIN RSA PRIVATE KEY-----\nMIIEvQIBADANBg\n-----END RSA PRIVATE KEY-----", "[masked]"},
		// none of these is a secret, and masking them would make the log useless
		{"the key idea is caching; the monkey typed 3 tokens; keyboard layout",
			"the key idea is caching; the monkey typed 3 tokens; keyboard layout"},
		{`git commit -m "fix the token parser"`, `git commit -m "fix the token parser"`},
		{"go test -run TestParseKeyEquals -count=1 ./...", "go test -run TestParseKeyEquals -count=1 ./..."},
	}
	for _, c := range cases {
		if got := redact(c.in); got != c.want {
			t.Errorf("redact(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
