package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func TestFramingBoundsInitialAndPartialRequests(t *testing.T) {
	for _, payload := range []string{"{\"op\":\"paste\",\"text\":\"" + strings.Repeat("x", MaxRequestBytes), "{\"op\":\"paste\""} {
		a, b := net.Pipe()
		reader := NewRequestReader(a, 50*time.Millisecond)
		go func() { _, _ = b.Write([]byte(payload)) }()
		_, err := reader.Read()
		if err == nil {
			t.Fatal("unbounded or unfinished frame accepted")
		}
		if len(payload) > MaxRequestBytes && !errors.Is(err, ErrFrameTooLarge) {
			t.Fatalf("wrong oversized error: %v", err)
		}
		a.Close()
		b.Close()
	}
}

func TestLargePasteTransportPreservesSingleLogicalPayload(t *testing.T) {
	text := strings.Repeat("界\r\n\x7f\x1b[D", 20000)
	var wire bytes.Buffer
	if err := EncodeRequest(json.NewEncoder(&wire), Request{Op: "paste", PaneID: 7, Text: text}); err != nil {
		t.Fatal(err)
	}
	var assembly PasteAssembly
	decoder := json.NewDecoder(&wire)
	completed := 0
	for decoder.More() {
		var r Request
		if err := decoder.Decode(&r); err != nil {
			t.Fatal(err)
		}
		request, ready, err := assembly.Accept(r)
		if err != nil {
			t.Fatal(err)
		}
		if ready {
			completed++
			if request.Op != "paste" || request.PaneID != 7 || request.Text != text {
				t.Fatal("paste payload or ownership changed")
			}
		}
	}
	if completed != 1 {
		t.Fatalf("paste became %d logical operations", completed)
	}
	if err := EncodeRequest(json.NewEncoder(&wire), Request{Op: "paste", Text: strings.Repeat("x", MaxInputBytes+1)}); err == nil {
		t.Fatal("oversized paste accepted")
	}
}

func TestIncompletePasteCannotReachApplication(t *testing.T) {
	var assembly PasteAssembly
	_, _, _ = assembly.Accept(Request{Op: "paste-begin", Size: 5})
	_, _, _ = assembly.Accept(Request{Op: "paste-chunk", Text: "abc"})
	if _, ready, err := assembly.Accept(Request{Op: "paste-end"}); ready || err == nil {
		t.Fatal("partial paste reached application")
	}
}
