package appserver

import (
	"bytes"
	"testing"
)

func TestFrameWriterAndReaderRoundTrip(t *testing.T) {
	var out bytes.Buffer
	writer := newFrameWriter(&out)
	if err := writer.writeJSON(map[string]string{"hello": "world"}); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}

	reader := newFrameReader(bytes.NewReader(out.Bytes()))
	frame, err := reader.readFrame()
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if got, want := string(frame), `{"hello":"world"}`; got != want {
		t.Fatalf("frame = %q, want %q", got, want)
	}
}
