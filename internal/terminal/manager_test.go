package terminal

import (
	"bytes"
	"testing"
)

func TestRingBufferTruncatesOldOutput(t *testing.T) {
	r := newRingBuffer(8)
	r.Write([]byte("12345"))
	r.Write([]byte("67890"))
	got, truncated := r.Bytes()
	if !truncated {
		t.Fatal("expected truncated flag")
	}
	if !bytes.Equal(got, []byte("34567890")) {
		t.Fatalf("unexpected data %q", got)
	}
}

func TestControlTransferRequiresApproval(t *testing.T) {
	s := &Session{subs: make(map[string]chan Event), buffer: newRingBuffer(8)}
	firstEvents, _, _ := s.Subscribe("first")
	s.Subscribe("second")
	if s.RequestControl("second") {
		t.Fatal("online controller was silently replaced")
	}
	select {
	case event := <-firstEvents:
		if event.Type != "control_request" || event.Requester != "second" {
			t.Fatalf("unexpected request event: %+v", event)
		}
	default:
		t.Fatal("controller did not receive transfer request")
	}
	if !s.RespondControl("first", "second", true) || !s.IsController("second") {
		t.Fatal("approved control transfer failed")
	}
}

func TestControlReleaseGrantsWaitingClient(t *testing.T) {
	s := &Session{subs: make(map[string]chan Event), buffer: newRingBuffer(8)}
	s.Subscribe("first")
	s.Subscribe("second")
	s.RequestControl("second")
	if !s.ReleaseControl("first") || !s.IsController("second") {
		t.Fatal("releasing control did not grant waiting client")
	}
}

func TestValidTerminalColor(t *testing.T) {
	for _, value := range []string{"#111416", "#D8DED9", "#000000", "#ffffff"} {
		if !validTerminalColor(value) {
			t.Fatalf("expected valid terminal color %q", value)
		}
	}
	for _, value := range []string{"111416", "#fff", "#1234567", "#12gg45", "#12345; echo bad"} {
		if validTerminalColor(value) {
			t.Fatalf("expected invalid terminal color %q", value)
		}
	}
}

func TestPlatformFromName(t *testing.T) {
	for input, expected := range map[string]string{
		"Linux\n":       "linux",
		"Darwin":        "macos",
		"MINGW64_NT":    "windows",
		"FreeBSD":       "bsd",
		"SunOS":         "unix",
		"  OpenBSD  \n": "bsd",
		"":              "",
	} {
		if actual := platformFromName(input); actual != expected {
			t.Errorf("platformFromName(%q)=%q, want %q", input, actual, expected)
		}
	}
}
