package app

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/session"
)

// slotWait bounds a save or a load: a long conversation's cache is gigabytes for the server's disk
const slotWait = 5 * time.Minute

// cacheFile is the name llama.cpp keeps one conversation's cache under, inside its --slot-save-path
func cacheFile(sessionFile string) string { return "ykode-" + session.ID(sessionFile) + ".bin" }

// cacheSlot is the slot a conversation is in, which ykode can only know when the server has just one
func cacheSlot(ctx context.Context, client *llm.Client, out io.Writer) (int, bool) {
	if n := client.SlotCount(ctx); n != 1 {
		_, _ = fmt.Fprintf(out, "ykode: save_server_cache needs a server with one slot, and this one has %s\n", slotsSaid(n))
		return 0, false
	}
	return 0, true
}

func slotsSaid(n int) string {
	if n == 0 {
		return "not said how many"
	}
	return fmt.Sprint(n)
}

// restoreCache loads what an earlier run saved, so --continue reads only what is new; failing costs a cold read
func restoreCache(client *llm.Client, sessionFile string, out io.Writer) {
	ctx, cancel := context.WithTimeout(context.Background(), slotWait)
	defer cancel()
	slot, ok := cacheSlot(ctx, client, out)
	if !ok {
		return
	}
	if err := client.RestoreSlot(ctx, slot, cacheFile(sessionFile)); err != nil {
		_, _ = fmt.Fprintf(out, "ykode: the server did not load this conversation's cache, so it reads it again: %v\n", err)
		return
	}
	_, _ = fmt.Fprintln(out, "ykode: the server loaded this conversation's cache")
}

// saveCache asks the server to keep this conversation's cache for the next --continue
func saveCache(client *llm.Client, sessionFile string, out io.Writer) {
	ctx, cancel := context.WithTimeout(context.Background(), slotWait)
	defer cancel()
	slot, ok := cacheSlot(ctx, client, out)
	if !ok {
		return
	}
	_, _ = fmt.Fprintln(out, "ykode: saving the server's cache of this conversation…")
	if err := client.SaveSlot(ctx, slot, cacheFile(sessionFile)); err != nil {
		_, _ = fmt.Fprintf(out, "ykode: the server did not save its cache: %v\n", err)
		return
	}
	_, _ = fmt.Fprintf(out, "ykode: the server saved its cache as %s\n", cacheFile(sessionFile))
}
