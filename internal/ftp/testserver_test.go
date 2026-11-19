package ftp

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
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
	tlsConfig *tls.Config
	tlsAuths  int
	tlsData   int
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

// enableTLS makes the server speak explicit FTPS for the next connections.
func (s *testFTPServer) enableTLS(t *testing.T) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "downloadx-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	s.tlsConfig = &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
		MinVersion:   tls.VersionTLS12,
	}
}

func (s *testFTPServer) tlsCounts() (auths, data int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tlsAuths, s.tlsData
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

type dataResult struct {
	conn net.Conn
	err  error
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
		dataCh  chan dataResult
		dataCon net.Conn
		rest    int64
		protP   bool
	)
	cleanupData := func() {
		if dataCon != nil {
			_ = dataCon.Close()
			dataCon = nil
		}
	}
	defer cleanupData()

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		verb, arg, _ := strings.Cut(line, " ")
		verb = strings.ToUpper(verb)

		switch verb {
		case "AUTH":
			if strings.EqualFold(arg, "TLS") && s.tlsConfig != nil {
				reply("234 AUTH TLS successful")
				tc := tls.Server(c, s.tlsConfig)
				if err := tc.Handshake(); err != nil {
					return
				}
				c = tc
				r = bufio.NewReader(c)
				w = bufio.NewWriter(c)
				s.mu.Lock()
				s.tlsAuths++
				s.mu.Unlock()
			} else {
				reply("502 AUTH not supported")
			}
		case "PBSZ":
			reply("200 PBSZ set to 0")
		case "PROT":
			if strings.EqualFold(arg, "P") && s.tlsConfig != nil {
				protP = true
				reply("200 PROT P accepted")
			} else {
				reply("536 PROT not supported")
			}
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
			cleanupData()
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				reply("425 cannot listen")
				continue
			}
			dataCh = make(chan dataResult, 1)
			go s.acceptData(ln, dataCh, protP)
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
			if dataCh == nil {
				reply("425 use EPSV or PASV first")
				continue
			}
			var res dataResult
			select {
			case res = <-dataCh:
			case <-time.After(5 * time.Second):
				res.err = fmt.Errorf("data connection timeout")
			}
			dataCh = nil
			if res.err != nil {
				reply("425 %v", res.err)
				continue
			}
			dataCon = res.conn
			reply("150 opening data connection")
			offset := rest
			rest = 0
			if offset < 0 {
				offset = 0
			}
			if offset > int64(len(s.data)) {
				offset = int64(len(s.data))
			}
			_, _ = dataCon.Write(s.data[offset:])
			cleanupData()
			reply("226 transfer complete")
		case "QUIT":
			reply("221 bye")
			return
		default:
			reply("500 unknown command %s", verb)
		}
	}
}

// acceptData accepts the client's passive data connection immediately, as
// real FTP servers do. Explicit FTPS protects the data channel before RETR.
func (s *testFTPServer) acceptData(ln net.Listener, ch chan<- dataResult, secure bool) {
	defer ln.Close()
	_ = ln.(*net.TCPListener).SetDeadline(time.Now().Add(5 * time.Second))
	conn, err := ln.Accept()
	if err != nil {
		ch <- dataResult{err: err}
		return
	}
	if secure {
		tc := tls.Server(conn, s.tlsConfig)
		if err := tc.Handshake(); err != nil {
			_ = conn.Close()
			ch <- dataResult{err: err}
			return
		}
		conn = tc
		s.mu.Lock()
		s.tlsData++
		s.mu.Unlock()
	}
	s.mu.Lock()
	s.conns[conn] = struct{}{}
	s.mu.Unlock()
	ch <- dataResult{conn: conn}
}
