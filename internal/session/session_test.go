package session

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/service-lasso/service-lasso-pty/internal/protocol"
	"github.com/service-lasso/service-lasso-pty/internal/pty"
)

type frameSink struct {
	mu     sync.Mutex
	frames []protocol.OutFrame
}

func (s *frameSink) add(frame protocol.OutFrame) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.frames = append(s.frames, frame)
}

func (s *frameSink) snapshot() []protocol.OutFrame {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]protocol.OutFrame(nil), s.frames...)
}

func readFrames(t *testing.T, reader io.Reader) *frameSink {
	t.Helper()
	sink := &frameSink{}
	go func() {
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 0, 64<<10), protocol.MaxFrameLine)
		for scanner.Scan() {
			var frame protocol.OutFrame
			if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
				return
			}
			sink.add(frame)
		}
	}()
	return sink
}

func waitFor(t *testing.T, sink *frameSink, predicate func([]protocol.OutFrame) bool, timeout time.Duration) []protocol.OutFrame {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		frames := sink.snapshot()
		if predicate(frames) {
			return frames
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for frames; saw %#v", sink.snapshot())
	return nil
}

func hasType(frames []protocol.OutFrame, want protocol.OutType) bool {
	for _, frame := range frames {
		if frame.Type == want {
			return true
		}
	}
	return false
}

func outputText(frames []protocol.OutFrame) string {
	var text string
	for _, frame := range frames {
		if frame.Type != protocol.OutData {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(frame.Data)
		if err == nil {
			text += string(decoded)
		}
	}
	return text
}

func exitFrame(frames []protocol.OutFrame) *protocol.OutFrame {
	for index := range frames {
		if frames[index].Type == protocol.OutExit {
			return &frames[index]
		}
	}
	return nil
}

func echoSpec(t *testing.T) pty.Spec {
	t.Helper()
	if runtime.GOOS == "windows" {
		return pty.Spec{Command: "cmd.exe", Args: []string{"/d", "/c", "echo pty-marker"}, Size: pty.Size{Rows: 24, Cols: 80}}
	}
	return pty.Spec{Command: "/bin/sh", Args: []string{"-c", "printf pty-marker"}, Size: pty.Size{Rows: 24, Cols: 80}}
}

func TestRunStreamsOutputAndExit(t *testing.T) {
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	t.Cleanup(func() { _ = inputWriter.Close(); _ = inputReader.Close() })
	sink := readFrames(t, outputReader)

	go func() {
		_, _ = Run(echoSpec(t), inputReader, outputWriter)
		_ = outputWriter.Close()
	}()

	frames := waitFor(t, sink, func(frames []protocol.OutFrame) bool {
		return exitFrame(frames) != nil
	}, 20*time.Second)

	if !hasType(frames, protocol.OutReady) {
		t.Fatalf("missing ready frame: %#v", frames)
	}
	if text := outputText(frames); !strings.Contains(text, "pty-marker") {
		t.Fatalf("missing output marker: %q", text)
	}
	exit := exitFrame(frames)
	if exit == nil || exit.Code == nil || *exit.Code != 0 {
		t.Fatalf("exit frame = %#v", exit)
	}
}

func TestRunAcceptsInput(t *testing.T) {
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	t.Cleanup(func() { _ = inputWriter.Close(); _ = inputReader.Close() })
	sink := readFrames(t, outputReader)

	spec := pty.Spec{Command: "/bin/sh", Args: []string{"-c", `read line; printf 'got:%s\n' "$line"`}, Size: pty.Size{Rows: 24, Cols: 80}}
	lineEnding := "\n"
	if runtime.GOOS == "windows" {
		spec = pty.Spec{Command: "cmd.exe", Args: []string{"/d", "/v:on", "/c", "set /p x=& echo got:!x!"}, Size: pty.Size{Rows: 24, Cols: 80}}
		lineEnding = "\r"
	}

	go func() {
		_, _ = Run(spec, inputReader, outputWriter)
		_ = outputWriter.Close()
	}()

	waitFor(t, sink, func(frames []protocol.OutFrame) bool { return hasType(frames, protocol.OutReady) }, 20*time.Second)
	payload := base64.StdEncoding.EncodeToString([]byte("hello" + lineEnding))
	if _, err := io.WriteString(inputWriter, `{"t":"input","b":"`+payload+`"}`+"\n"); err != nil {
		t.Fatal(err)
	}

	frames := waitFor(t, sink, func(frames []protocol.OutFrame) bool {
		return strings.Contains(outputText(frames), "got:hello")
	}, 20*time.Second)
	if !strings.Contains(outputText(frames), "got:hello") {
		t.Fatalf("input was not echoed by the process: %q", outputText(frames))
	}
	_ = inputWriter.Close()
}

func TestRunTerminatesWhenParentDisconnects(t *testing.T) {
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	sink := readFrames(t, outputReader)

	spec := pty.Spec{Command: "/bin/sh", Args: []string{"-c", "sleep 60"}, Size: pty.Size{Rows: 24, Cols: 80}}
	if runtime.GOOS == "windows" {
		spec = pty.Spec{Command: "cmd.exe", Args: []string{"/d", "/c", "ping -n 60 127.0.0.1 >NUL"}, Size: pty.Size{Rows: 24, Cols: 80}}
	}

	done := make(chan struct{})
	go func() {
		_, _ = Run(spec, inputReader, outputWriter)
		_ = outputWriter.Close()
		close(done)
	}()

	waitFor(t, sink, func(frames []protocol.OutFrame) bool { return hasType(frames, protocol.OutReady) }, 20*time.Second)
	start := time.Now()
	_ = inputWriter.Close()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("helper did not terminate after parent stdin closed")
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Fatalf("termination took %s", elapsed)
	}
	frames := sink.snapshot()
	if exit := exitFrame(frames); exit == nil {
		t.Fatalf("missing exit frame: %#v", frames)
	}
}
