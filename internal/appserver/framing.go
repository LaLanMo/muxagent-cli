package appserver

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

type frameReader struct {
	reader *bufio.Reader
}

func newFrameReader(r io.Reader) *frameReader {
	return &frameReader{reader: bufio.NewReader(r)}
}

func (r *frameReader) readFrame() ([]byte, error) {
	length := -1
	for {
		line, err := r.reader.ReadString('\n')
		if err != nil {
			if err == io.EOF && line == "" && length < 0 {
				return nil, io.EOF
			}
			return nil, err
		}
		if line == "\r\n" {
			break
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("invalid frame header %q", line)
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			parsed, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, fmt.Errorf("invalid content length %q", value)
			}
			length = parsed
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r.reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

type frameWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func newFrameWriter(w io.Writer) *frameWriter {
	return &frameWriter{w: w}
}

func (w *frameWriter) writeJSON(v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return w.writeFrame(payload)
}

func (w *frameWriter) writeFrame(payload []byte) error {
	var frame bytes.Buffer
	if _, err := fmt.Fprintf(&frame, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
		return err
	}
	if _, err := frame.Write(payload); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err := w.w.Write(frame.Bytes())
	return err
}
