package session

import (
	"io"
	"sync/atomic"
	"time"

	"github.com/service-lasso/service-lasso-pty/internal/protocol"
	"github.com/service-lasso/service-lasso-pty/internal/pty"
)

// Run starts the managed process on a pseudo terminal, streams the terminal
// output in data frames, applies input and resize frames, and reports the
// managed exit status. Parent stdin EOF terminates the managed process: the
// helper never outlives the runtime that owns it.
func Run(spec pty.Spec, in io.Reader, out io.Writer) (int, error) {
	writer := protocol.NewWriter(out)
	process, err := pty.Start(spec)
	if err != nil {
		line, encodeErr := protocol.EncodeError(err.Error())
		if encodeErr == nil {
			_ = writer.Write(line)
		}
		return 1, err
	}

	line, err := protocol.EncodeReady(process.PID())
	if err != nil {
		_ = process.Terminate()
		_, _ = process.Wait()
		_ = process.Close()
		return 1, err
	}
	if err := writer.Write(line); err != nil {
		_ = process.Terminate()
		_, _ = process.Wait()
		_ = process.Close()
		return 1, err
	}

	var lastOutput atomic.Int64
	lastOutput.Store(time.Now().UnixNano())
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		buffer := make([]byte, protocol.ChunkSize)
		for {
			count, readErr := process.Read(buffer)
			if count > 0 {
				lastOutput.Store(time.Now().UnixNano())
				if frame, encodeErr := protocol.EncodeData(buffer[:count]); encodeErr == nil {
					if writer.Write(frame) != nil {
						return
					}
				}
			}
			if readErr != nil {
				return
			}
		}
	}()

	go func() {
		scanner := protocol.Reader(in)
		for scanner.Scan() {
			frame, data, decodeErr := protocol.DecodeInput(scanner.Bytes())
			if decodeErr != nil {
				break
			}
			switch frame.Type {
			case protocol.InInput:
				if _, writeErr := process.Write(data); writeErr != nil {
					_ = process.Terminate()
					return
				}
			case protocol.InResize:
				_ = process.Resize(pty.Size{Rows: frame.Rows, Cols: frame.Cols})
			case protocol.InKill:
				_ = process.Terminate()
			}
		}
		_ = process.Terminate()
	}()

	exit, waitErr := process.Wait()
	// ConPTY can flush buffered output shortly after the managed process exits.
	// Drain until the stream has been quiet for a bounded interval, but never
	// less than one quiet window after the exit itself.
	exitAt := time.Now().UnixNano()
	for {
		last := lastOutput.Load()
		if last < exitAt {
			last = exitAt
		}
		if time.Since(time.Unix(0, last)) >= 150*time.Millisecond {
			break
		}
		if time.Since(time.Unix(0, exitAt)) > 2*time.Second {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = process.Close()
	<-readerDone

	if frame, encodeErr := protocol.EncodeExit(exit.Code, exit.Signal); encodeErr == nil {
		_ = writer.Write(frame)
	}
	if waitErr != nil {
		return 1, waitErr
	}
	if exit.Code < 0 || exit.Code > 255 {
		return 1, nil
	}
	return exit.Code, nil
}
