package client

import "testing"

func TestDisplayMessageShowsOnlyNewMessages(t *testing.T) {
	u := pickerUI(t)
	u.picker = false
	frame := func(seq uint64, message string) {
		f := splitFrame()
		f.Message, f.MessageSeq = message, seq
		u.applyFrame(f)
	}
	frame(4, "sent before this client attached")
	if u.message != "" {
		t.Fatalf("an old message showed on attach: %q", u.message)
	}
	frame(5, "build done")
	if u.message != "build done" {
		t.Fatalf("new message: %q", u.message)
	}
	u.message = ""
	frame(5, "build done")
	if u.message != "" {
		t.Fatalf("the same message showed twice: %q", u.message)
	}
}
