package protocol

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// MaxFrameLine bounds one JSON line in either direction. Output chunks are at
// most ChunkSize bytes before base64 encoding; input frames are validated
// against the same bound so a peer cannot force unbounded buffering.
const MaxFrameLine = 1 << 20

const ChunkSize = 32 << 10

// OutType identifies a helper-to-parent frame.
type OutType string

const (
	OutReady OutType = "ready"
	OutData  OutType = "data"
	OutExit  OutType = "exit"
	OutError OutType = "error"
)

// InType identifies a parent-to-helper frame.
type InType string

const (
	InInput  InType = "input"
	InResize InType = "resize"
	InKill   InType = "kill"
)

type OutFrame struct {
	Type    OutType `json:"t"`
	PID     int     `json:"pid,omitempty"`
	Data    string  `json:"b,omitempty"`
	Code    *int    `json:"code,omitempty"`
	Signal  string  `json:"signal,omitempty"`
	Message string  `json:"message,omitempty"`
}

type InFrame struct {
	Type InType `json:"t"`
	Data string `json:"b,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
}

func EncodeData(chunk []byte) ([]byte, error) {
	line, err := json.Marshal(OutFrame{Type: OutData, Data: base64.StdEncoding.EncodeToString(chunk)})
	if err != nil {
		return nil, err
	}
	if len(line) > MaxFrameLine {
		return nil, fmt.Errorf("encoded frame exceeds %d bytes", MaxFrameLine)
	}
	return append(line, '\n'), nil
}

func EncodeReady(pid int) ([]byte, error) {
	return encodeOut(OutFrame{Type: OutReady, PID: pid})
}

func EncodeExit(code int, signal string) ([]byte, error) {
	return encodeOut(OutFrame{Type: OutExit, Code: &code, Signal: signal})
}

func EncodeError(message string) ([]byte, error) {
	return encodeOut(OutFrame{Type: OutError, Message: message})
}

func encodeOut(frame OutFrame) ([]byte, error) {
	line, err := json.Marshal(frame)
	if err != nil {
		return nil, err
	}
	if len(line) > MaxFrameLine {
		return nil, fmt.Errorf("encoded frame exceeds %d bytes", MaxFrameLine)
	}
	return append(line, '\n'), nil
}

// DecodeInput validates one parent frame and returns its raw input bytes.
func DecodeInput(line []byte) (InFrame, []byte, error) {
	if len(line) > MaxFrameLine {
		return InFrame{}, nil, fmt.Errorf("frame exceeds %d bytes", MaxFrameLine)
	}
	var frame InFrame
	if err := json.Unmarshal(line, &frame); err != nil {
		return InFrame{}, nil, fmt.Errorf("decode frame: %w", err)
	}
	switch frame.Type {
	case InInput:
		data, err := base64.StdEncoding.DecodeString(frame.Data)
		if err != nil {
			return InFrame{}, nil, fmt.Errorf("decode input: %w", err)
		}
		if len(data) == 0 {
			return InFrame{}, nil, errors.New("input frame is empty")
		}
		return frame, data, nil
	case InResize:
		if frame.Rows == 0 || frame.Cols == 0 {
			return InFrame{}, nil, errors.New("resize frame requires rows and cols")
		}
		return frame, nil, nil
	case InKill:
		return frame, nil, nil
	default:
		return InFrame{}, nil, fmt.Errorf("unsupported frame type %q", frame.Type)
	}
}

// Reader returns a line scanner with the frame bound applied.
func Reader(r io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), MaxFrameLine)
	return scanner
}

// Writer serializes frames from concurrent producers.
type Writer struct {
	mu sync.Mutex
	w  io.Writer
}

func NewWriter(w io.Writer) *Writer { return &Writer{w: w} }

func (w *Writer) Write(line []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err := w.w.Write(line)
	return err
}
