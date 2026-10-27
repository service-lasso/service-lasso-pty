package protocol

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestDataFrameRoundTrip(t *testing.T) {
	payload := []byte("hello\x1b[31mred\x1b[0m")
	line, err := EncodeData(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(line, []byte("\n")) {
		t.Fatalf("frame is not newline terminated: %q", line)
	}
	var frame OutFrame
	if err := json.Unmarshal(bytes.TrimSpace(line), &frame); err != nil {
		t.Fatal(err)
	}
	if frame.Type != OutData {
		t.Fatalf("type = %q", frame.Type)
	}
	decoded, err := base64.StdEncoding.DecodeString(frame.Data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, payload) {
		t.Fatalf("decoded = %q", decoded)
	}
}

func TestDecodeInputValidatesFrames(t *testing.T) {
	input := base64.StdEncoding.EncodeToString([]byte("ping\r"))
	frame, data, err := DecodeInput([]byte(`{"t":"input","b":"` + input + `"}`))
	if err != nil {
		t.Fatal(err)
	}
	if frame.Type != InInput || string(data) != "ping\r" {
		t.Fatalf("frame = %#v data = %q", frame, data)
	}

	resize, _, err := DecodeInput([]byte(`{"t":"resize","rows":40,"cols":120}`))
	if err != nil || resize.Type != InResize || resize.Rows != 40 || resize.Cols != 120 {
		t.Fatalf("resize = %#v err = %v", resize, err)
	}

	for _, invalid := range []string{
		`{"t":"input","b":""}`,
		`{"t":"input","b":"not base64!"}`,
		`{"t":"resize","rows":0,"cols":120}`,
		`{"t":"unknown"}`,
		`not json`,
	} {
		if _, _, err := DecodeInput([]byte(invalid)); err == nil {
			t.Fatalf("expected rejection for %q", invalid)
		}
	}

	oversized := []byte(`{"t":"input","b":"` + strings.Repeat("A", MaxFrameLine) + `"}`)
	if _, _, err := DecodeInput(oversized); err == nil {
		t.Fatal("expected oversized frame rejection")
	}
}

func TestExitFrameCarriesCode(t *testing.T) {
	line, err := EncodeExit(3, "")
	if err != nil {
		t.Fatal(err)
	}
	var frame OutFrame
	if err := json.Unmarshal(line, &frame); err != nil {
		t.Fatal(err)
	}
	if frame.Type != OutExit || frame.Code == nil || *frame.Code != 3 {
		t.Fatalf("exit frame = %#v", frame)
	}
}
