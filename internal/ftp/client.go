package ftp

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Options configures an FTP session.
type Options struct {
	User     string
	Pass     string
	TLS      bool
	Insecure bool
	Timeout  time.Duration
}

// Session is one FTP control connection.
type Session struct {
	conn   net.Conn
	r      *bufio.Reader
	w      *bufio.Writer
	opts   Options
	host   string
	secure bool
}

// Dial connects and reads the server greeting. For explicit FTPS, it then
// negotiates AUTH TLS and protects the control channel.
func Dial(ctx context.Context, addr string, opts Options) (*Session, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}
	dialer := net.Dialer{Timeout: opts.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	s := &Session{
		conn: conn,
		r:    bufio.NewReader(conn),
		w:    bufio.NewWriter(conn),
		opts: opts,
		host: hostOnly(addr),
	}
	s.deadline()
	rep, err := readReply(s.r)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if rep.Code != 220 && rep.Code != 120 {
		conn.Close()
		return nil, &Error{Command: "greeting", Code: rep.Code, Message: rep.Message}
	}
	if opts.TLS {
		if err := s.upgradeTLS(ctx); err != nil {
			conn.Close()
			return nil, err
		}
	}
	return s, nil
}

func hostOnly(addr string) string {
	h, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return h
}

func (s *Session) deadline() {
	if s.opts.Timeout > 0 {
		_ = s.conn.SetDeadline(time.Now().Add(s.opts.Timeout))
	}
}

func (s *Session) cmd(format string, args ...any) (Reply, error) {
	line := fmt.Sprintf(format, args...)
	if strings.ContainsAny(line, "\r\n") {
		return Reply{}, errors.New("ftp: command contains CR/LF")
	}
	s.deadline()
	if _, err := s.w.WriteString(line + "\r\n"); err != nil {
		return Reply{}, err
	}
	if err := s.w.Flush(); err != nil {
		return Reply{}, err
	}
	return readReply(s.r)
}

func (s *Session) expect(want int, command, format string, args ...any) (Reply, error) {
	rep, err := s.cmd(format, args...)
	if err != nil {
		return rep, err
	}
	if rep.Code != want {
		return rep, &Error{Command: command, Code: rep.Code, Message: rep.Message}
	}
	return rep, nil
}

func (s *Session) upgradeTLS(ctx context.Context) error {
	s.deadline()
	rep, err := s.cmd("AUTH TLS")
	if err != nil {
		return err
	}
	if rep.Code != 234 {
		return &Error{Command: "AUTH TLS", Code: rep.Code, Message: rep.Message}
	}
	tlsConn := tls.Client(s.conn, &tls.Config{
		ServerName:         s.host,
		InsecureSkipVerify: s.opts.Insecure,
	})
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return fmt.Errorf("ftp TLS handshake: %w", err)
	}
	s.conn = tlsConn
	s.r = bufio.NewReader(tlsConn)
	s.w = bufio.NewWriter(tlsConn)
	s.secure = true
	if _, err := s.expect(200, "PBSZ", "PBSZ 0"); err != nil {
		return err
	}
	if _, err := s.expect(200, "PROT", "PROT P"); err != nil {
		return err
	}
	return nil
}

// Login authenticates; empty credentials mean anonymous.
func (s *Session) Login() error {
	user := s.opts.User
	if user == "" {
		user = "anonymous"
	}
	pass := s.opts.Pass
	if pass == "" && strings.EqualFold(user, "anonymous") {
		pass = "anonymous@"
	}
	rep, err := s.cmd("USER %s", user)
	if err != nil {
		return err
	}
	switch rep.Code {
	case 230, 202:
		return nil
	case 331:
		rep, err = s.cmd("PASS %s", pass)
		if err != nil {
			return err
		}
		if rep.Code != 230 && rep.Code != 202 {
			return &Error{Command: "PASS", Code: rep.Code, Message: rep.Message}
		}
		return nil
	default:
		return &Error{Command: "USER", Code: rep.Code, Message: rep.Message}
	}
}

// SetBinary switches the transfer type to image (binary).
func (s *Session) SetBinary() error {
	_, err := s.expect(200, "TYPE", "TYPE I")
	return err
}

// Size returns the remote file size.
func (s *Session) Size(path string) (int64, error) {
	rep, err := s.cmd("SIZE %s", path)
	if err != nil {
		return 0, err
	}
	if rep.Code != 213 {
		return 0, &Error{Command: "SIZE", Code: rep.Code, Message: rep.Message}
	}
	n, err := strconv.ParseInt(strings.TrimSpace(rep.Message), 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("ftp: bad SIZE reply %q", rep.Message)
	}
	return n, nil
}

// ModTime returns the remote modification time when the server supports MDTM.
func (s *Session) ModTime(path string) (time.Time, error) {
	rep, err := s.cmd("MDTM %s", path)
	if err != nil {
		return time.Time{}, err
	}
	if rep.Code != 213 {
		return time.Time{}, &Error{Command: "MDTM", Code: rep.Code, Message: rep.Message}
	}
	v := strings.TrimSpace(rep.Message)
	if len(v) > 14 {
		v = v[:14]
	}
	t, err := time.Parse("20060102150405", v)
	if err != nil {
		return time.Time{}, fmt.Errorf("ftp: bad MDTM reply %q", rep.Message)
	}
	return t.UTC(), nil
}

// OpenRanged starts a RETR transfer at offset and returns the data stream.
// Closing the reader (or reading to EOF) consumes the final completion reply.
func (s *Session) OpenRanged(ctx context.Context, path string, offset int64) (io.ReadCloser, error) {
	if offset > 0 {
		rep, err := s.cmd("REST %d", offset)
		if err != nil {
			return nil, err
		}
		if rep.Code != 350 {
			if isRESTUnsupported(rep.Code) {
				return nil, ErrNoREST
			}
			return nil, &Error{Command: "REST", Code: rep.Code, Message: rep.Message}
		}
	}

	data, err := s.openData(ctx)
	if err != nil {
		return nil, err
	}
	rep, err := s.cmd("RETR %s", path)
	if err != nil {
		data.Close()
		return nil, err
	}
	if rep.Code != 125 && rep.Code != 150 {
		data.Close()
		return nil, &Error{Command: "RETR " + path, Code: rep.Code, Message: rep.Message}
	}
	return &dataReader{conn: data, s: s}, nil
}

func isRESTUnsupported(code int) bool {
	switch code {
	case 500, 501, 502, 504:
		return true
	}
	return false
}

func (s *Session) openData(ctx context.Context) (net.Conn, error) {
	var port int
	if rep, err := s.cmd("EPSV"); err == nil && rep.Code == 229 {
		if p, perr := parseEPSV(rep.Message); perr == nil {
			port = p
		}
	}
	if port == 0 {
		rep, err := s.cmd("PASV")
		if err != nil {
			return nil, err
		}
		if rep.Code != 227 {
			return nil, &Error{Command: "PASV", Code: rep.Code, Message: rep.Message}
		}
		_, p, perr := parsePASV(rep.Message)
		if perr != nil {
			return nil, perr
		}
		port = p
	}

	dialer := net.Dialer{Timeout: s.opts.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(s.host, strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	if s.secure {
		tlsConn := tls.Client(conn, &tls.Config{
			ServerName:         s.host,
			InsecureSkipVerify: s.opts.Insecure,
		})
		hsCtx, cancel := context.WithTimeout(ctx, s.opts.Timeout)
		defer cancel()
		if err := tlsConn.HandshakeContext(hsCtx); err != nil {
			conn.Close()
			return nil, fmt.Errorf("ftp data TLS handshake: %w", err)
		}
		conn = tlsConn
	}
	return conn, nil
}

// Close ends the session, sending QUIT on a best-effort basis.
func (s *Session) Close() error {
	if s == nil || s.conn == nil {
		return nil
	}
	_ = s.conn.SetDeadline(time.Now().Add(time.Second))
	_, _ = s.w.WriteString("QUIT\r\n")
	_ = s.w.Flush()
	return s.conn.Close()
}

// ErrNoREST reports a server that does not support REST (resumed transfers).
var ErrNoREST = errors.New("ftp: server does not support REST")

type dataReader struct {
	conn     net.Conn
	s        *Session
	once     sync.Once
	finalErr error
}

func (d *dataReader) Read(p []byte) (int, error) {
	if d.s.opts.Timeout > 0 {
		_ = d.conn.SetReadDeadline(time.Now().Add(d.s.opts.Timeout))
	}
	n, err := d.conn.Read(p)
	if err == io.EOF {
		d.finish()
	}
	return n, err
}

func (d *dataReader) Close() error {
	_ = d.conn.Close()
	d.finish()
	return d.finalErr
}

func (d *dataReader) finish() {
	d.once.Do(func() {
		d.s.deadline()
		rep, err := readReply(d.s.r)
		if err != nil {
			d.finalErr = err
			return
		}
		if rep.Code != 226 && rep.Code != 250 {
			d.finalErr = &Error{Command: "RETR", Code: rep.Code, Message: rep.Message}
		}
	})
}
