package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/neko233-com/DownloadX/internal/inbox"
)

func TestHandlePing(t *testing.T) {
	resp := handle(message{Action: "ping"})
	if !resp.OK || resp.Version == "" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestHandleDownloadValidation(t *testing.T) {
	if resp := handle(message{Action: "download", URL: "file:///etc/passwd"}); resp.OK || resp.Error == "" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestHandleDownloadQueuesRequest(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("DOWNLOADX_NO_LAUNCH", "1")
	resp := handle(message{Action: "download", URL: "https://example.com/file.zip", Filename: "file.zip"})
	if !resp.OK {
		t.Fatalf("resp = %+v", resp)
	}
	items, err := inbox.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Request.URL != "https://example.com/file.zip" {
		t.Fatalf("items = %+v", items)
	}
}

func TestNativeMessageRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := writeMessage(&buf, response{OK: true, Version: "0.2.0"}); err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(buf.Bytes()[:4]) == 0 {
		t.Fatal("zero length")
	}
	payload, err := readMessage(&buf)
	if err != nil {
		t.Fatal(err)
	}
	var resp response
	if err := json.Unmarshal(payload, &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.Version != "0.2.0" {
		t.Fatalf("resp = %+v", resp)
	}
}
