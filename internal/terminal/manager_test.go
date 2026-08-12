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
