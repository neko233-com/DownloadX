// Command downloadx-host is the native messaging host used by the DownloadX
// browser extensions in Chrome and Edge.
package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/neko233-com/DownloadX/internal/inbox"
	"github.com/neko233-com/DownloadX/internal/version"
)

type message struct {
	Action   string `json:"action"`
	URL      string `json:"url,omitempty"`
	Filename string `json:"filename,omitempty"`
	Referrer string `json:"referrer,omitempty"`
}

type response struct {
	OK      bool   `json:"ok"`
	Version string `json:"version,omitempty"`
	Error   string `json:"error,omitempty"`
}

func main() {
	if err := run(); err != nil {
		inbox.Logf("native host: %v", err)
		os.Exit(1)
	}
}

func run() error {
	for {
		payload, err := readMessage(os.Stdin)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			return err
		}
		var msg message
		if err := json.Unmarshal(payload, &msg); err != nil {
			_ = writeMessage(os.Stdout, response{Error: "invalid JSON"})
			continue
		}
		if err := writeMessage(os.Stdout, handle(msg)); err != nil {
			return err
		}
	}
}

func handle(msg message) response {
	switch msg.Action {
	case "ping":
		return response{OK: true, Version: version.Version}
	case "open":
		if err := launchGUI(); err != nil {
			return response{Error: err.Error()}
		}
		return response{OK: true}
	case "download":
		u, err := url.Parse(strings.TrimSpace(msg.URL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return response{Error: "only http and https downloads can be handed off"}
		}
		if err := inbox.Enqueue(inbox.Request{
			URL:      u.String(),
			Filename: msg.Filename,
			Referrer: msg.Referrer,
		}); err != nil {
			return response{Error: err.Error()}
		}
		if err := launchGUI(); err != nil {
			// The request is queued; it arrives when DownloadX opens.
			inbox.Logf("native host: launch GUI: %v", err)
		}
		return response{OK: true}
	default:
		return response{Error: "unknown action"}
	}
}

func readMessage(r io.Reader) ([]byte, error) {
	var size uint32
	if err := binary.Read(r, binary.LittleEndian, &size); err != nil {
		return nil, err
	}
	if size == 0 || size > 4<<20 {
		return nil, fmt.Errorf("native message size %d is invalid", size)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func writeMessage(w io.Writer, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var size [4]byte
	binary.LittleEndian.PutUint32(size[:], uint32(len(payload)))
	if _, err := w.Write(size[:]); err != nil {
		return err
	}
	_, err = w.Write(payload)
	return err
}

func launchGUI() error {
	if os.Getenv("DOWNLOADX_NO_LAUNCH") != "" {
		return nil
	}
	name := "downloadx-gui.exe"
	candidates := make([]string, 0, 2)
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), name))
	}
	if found, err := exec.LookPath(name); err == nil {
		candidates = append(candidates, found)
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			cmd := exec.Command(candidate)
			configureProcess(cmd)
			if err := cmd.Start(); err != nil {
				return err
			}
			return cmd.Process.Release()
		}
	}
	return errors.New("downloadx-gui.exe was not found next to the native host")
}
