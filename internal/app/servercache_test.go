package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// slotServer is llama.cpp with the given slot count, remembering each slot action and the file it named
func slotServer(t *testing.T, slots int, status int) (*llm.Client, *[]string) {
	t.Helper()
	var actions []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/props":
			fmt.Fprintf(w, `{"total_slots":%d}`, slots)
		case strings.HasPrefix(r.URL.Path, "/slots/"):
			var body struct{ Filename string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			actions = append(actions, r.URL.Path+"?"+r.URL.RawQuery+" "+body.Filename)
			if status != http.StatusOK {
				http.Error(w, `{"error":{"message":"slot save/restore is disabled"}}`, status)
				return
			}
			fmt.Fprint(w, `{}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return llm.NewClient(srv.URL + "/v1"), &actions
}

// leaving saves the slot under the conversation's id, and --continue loads that same file back
func TestTheServerCacheIsSavedAndLoadedUnderTheConversationsName(t *testing.T) {
	client, actions := slotServer(t, 1, http.StatusOK)
	var out strings.Builder
	saveCache(client, "/home/.ykode/sessions/s20261121-101500.000.jsonl", &out)
	restoreCache(client, "/home/.ykode/sessions/s20261121-101500.000.jsonl", &out)
	want := "/slots/0?action=save ykode-s20261121-101500.000.bin," +
		"/slots/0?action=restore ykode-s20261121-101500.000.bin"
	if got := strings.Join(*actions, ","); got != want {
		t.Errorf("slot actions %s, want %s", got, want)
	}
	if !strings.Contains(out.String(), "saved its cache") || !strings.Contains(out.String(), "loaded this conversation's cache") {
		t.Errorf("the run did not say what the server did:\n%s", out.String())
	}
}

// with several slots any of them may hold the conversation, so none is written over on a guess
func TestASeveralSlotServerIsLeftAlone(t *testing.T) {
	client, actions := slotServer(t, 4, http.StatusOK)
	var out strings.Builder
	saveCache(client, "s1.jsonl", &out)
	if len(*actions) != 0 || !strings.Contains(out.String(), "this one has 4") {
		t.Errorf("actions %v, said %q: want nothing touched and the reason given", *actions, out.String())
	}
}

// a server started without --slot-save-path refuses, and the run says why rather than failing
func TestARefusedSaveSaysWhyAndTheRunGoesOn(t *testing.T) {
	client, _ := slotServer(t, 1, http.StatusNotImplemented)
	var out strings.Builder
	saveCache(client, "s1.jsonl", &out)
	if !strings.Contains(out.String(), "did not save") || !strings.Contains(out.String(), "disabled") {
		t.Errorf("the refusal was not passed on: %q", out.String())
	}
}
