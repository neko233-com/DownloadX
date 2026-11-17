package ftp

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// testFTPServer is a tiny scripted FTP server used by the downloader tests.
// It supports USER/PASS/TYPE/EPSV/PASV/SIZE/MDTM/REST/RETR/QUIT.
type testFTPServer struct {
	ln        net.Listener
	data      []byte
	user      string
	pass      string
	sizeOK    bool
	restOK    bool
	missing   bool
	mu        sync.Mutex
	restSeen  []int64
	conns     map[net.Conn]struct{}
	wg        sync.WaitGroup
	closeOnce sync.Once
}

func newTestFTPServer(t *testing.T, data []byte) *testFTPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &testFTPServer{
		ln:     ln,
		data:   data,
		user:   "test",
		pass:   "test",
		sizeOK: true,
		restOK: true,
		conns:  map[net.Conn]struct{}{},
	}
	s.wg.Add(1)
	go s.acceptLoop()
	t.Cleanup(s.Close)
	return s
}

func (s *testFTPServer) URL(path string) string {
	return fmt.Sprintf("ftp://%s:%s@%s%s", s.user, s.pass, s.ln.Addr().String(), path)
}

func (s *testFTPServer) Close() {
	s.closeOnce.Do(func() {
		_ = s.ln.Close()
		s.mu.Lock()
		for c := range s.conns {
			_ = c.Close()
		}
		s.mu.Unlock()
		s.wg.Wait()
	})
}

func (s *testFTPServer) restOffsets() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int64(nil), s.restSeen...)
}

func (s *testFTPServer) acceptLoop() {
	defer s.wg.Done()
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns[c] = struct{}{}
		s.mu.Unlock()
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer func() {
				s.mu.Lock()
				delete(s.conns, c)
				s.mu.Unlock()
				_ = c.Close()
			}()
			s.handle(c)
		}()
	}
}

func (s *testFTPServer) handle(c net.Conn) {
	r := bufio.NewReader(c)
	w := bufio.NewWriter(c)
	reply := func(format string, args ...any) {
		fmt.Fprintf(w, format+"\r\n", args...)
		_ = w.Flush()
	}
	reply("220 test ftp ready")

	var (
		dataLn net.Listener
		rest   int64
	)
	defer func() {
		if dataLn != nil {
			_ = dataLn.Close()
		}
	}()

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		verb, arg, _ := strings.Cut(line, " ")
		verb = strings.ToUpper(verb)

		switch verb {
		case "USER":
			reply("331 password required")
		case "PASS":
			if arg == s.pass {
				reply("230 logged in")
			} else {
				reply("530 bad credentials")
			}
		case "TYPE":
			reply("200 type set to I")
		case "SIZE":
			if !s.sizeOK {
				reply("550 SIZE not supported")
				continue
			}
			reply("213 %d", len(s.data))
		case "MDTM":
			reply("213 20260101000000")
		case "REST":
			if !s.restOK {
				reply("502 REST not supported")
				continue
			}
			n, err := strconv.ParseInt(strings.TrimSpace(arg), 10, 64)
			if err != nil {
				reply("501 bad offset")
				continue
			}
			rest = n
			s.mu.Lock()
			s.restSeen = append(s.restSeen, n)
			s.mu.Unlock()
			reply("350 restart at %d", n)
		case "EPSV", "PASV":
			if dataLn != nil {
				_ = dataLn.Close()
			}
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				reply("425 cannot listen")
				continue
			}
			dataLn = ln
			port := ln.Addr().(*net.TCPAddr).Port
			if verb == "EPSV" {
				reply("229 Entering Extended Passive Mode (|||%d|)", port)
			} else {
				reply("227 Entering Passive Mode (127,0,0,1,%d,%d)", port/256, port%256)
			}
		case "RETR":
			if s.missing {
				reply("550 no such file")
				continue
			}
			if dataLn == nil {
				reply("425 use EPSV or PASV first")
				continue
			}
			_ = dataLn.(*net.TCPListener).SetDeadline(time.Now().Add(5 * time.Second))
			dconn, err := dataLn.Accept()
			if err != nil {
				reply("425 cannot accept")
				continue
			}
			_ = dataLn.Close()
			dataLn = nil
			reply("150 opening data connection")
			offset := rest
			rest = 0
			if offset < 0 {
				offset = 0
			}
			if offset > int64(len(s.data)) {
				offset = int64(len(s.data))
			}
			_, _ = dconn.Write(s.data[offset:])
			_ = dconn.Close()
			reply("226 transfer complete")
		case "QUIT":
			reply("221 bye")
			return
		default:
			reply("500 unknown command %s", verb)
		}
	}
}
