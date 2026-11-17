package ftp

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

// Reply is one parsed FTP response, possibly spanning multiple lines.
type Reply struct {
	Code    int
	Message string
}

// Error is a negative FTP completion reply.
type Error struct {
	Command string
	Code    int
	Message string
}

func (e *Error) Error() string {
	if e.Command == "" {
		return fmt.Sprintf("ftp %d %s", e.Code, e.Message)
	}
	return fmt.Sprintf("ftp %s: %d %s", e.Command, e.Code, e.Message)
}

func readReply(r *bufio.Reader) (Reply, error) {
	line, err := readLine(r)
	if err != nil {
		return Reply{}, err
	}
	if len(line) < 3 {
		return Reply{}, fmt.Errorf("ftp: short reply %q", line)
	}
	code, err := strconv.Atoi(line[:3])
	if err != nil {
		return Reply{}, fmt.Errorf("ftp: malformed reply %q", line)
	}
	msg := strings.TrimSpace(line[3:])
	if len(line) > 3 && line[3] == '-' {
		prefix := line[:3]
		var b strings.Builder
		b.WriteString(msg)
		for {
			l, err := readLine(r)
			if err != nil {
				return Reply{}, err
			}
			b.WriteByte('\n')
			if len(l) >= 4 && l[:3] == prefix && l[3] == ' ' {
				b.WriteString(strings.TrimSpace(l[4:]))
				break
			}
			b.WriteString(l)
		}
		msg = b.String()
	}
	return Reply{Code: code, Message: msg}, nil
}

func readLine(r *bufio.Reader) (string, error) {
	s, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(s, "\r\n"), nil
}

// parseEPSV extracts the port from "229 ... (|||port|)".
func parseEPSV(msg string) (int, error) {
	i := strings.Index(msg, "(|||")
	if i < 0 {
		return 0, fmt.Errorf("ftp: malformed EPSV reply %q", msg)
	}
	rest := msg[i+4:]
	j := strings.Index(rest, "|)")
	if j < 0 {
		return 0, fmt.Errorf("ftp: malformed EPSV reply %q", msg)
	}
	port, err := strconv.Atoi(rest[:j])
	if err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf("ftp: bad EPSV port %q", rest[:j])
	}
	return port, nil
}

// parsePASV extracts host and port from "227 ... (h1,h2,h3,h4,p1,p2)".
func parsePASV(msg string) (string, int, error) {
	i := strings.LastIndex(msg, "(")
	j := strings.LastIndex(msg, ")")
	if i < 0 || j <= i {
		return "", 0, fmt.Errorf("ftp: malformed PASV reply %q", msg)
	}
	parts := strings.Split(msg[i+1:j], ",")
	if len(parts) != 6 {
		return "", 0, fmt.Errorf("ftp: malformed PASV reply %q", msg)
	}
	nums := make([]int, 6)
	for k, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 0 || n > 255 {
			return "", 0, fmt.Errorf("ftp: malformed PASV reply %q", msg)
		}
		nums[k] = n
	}
	host := fmt.Sprintf("%d.%d.%d.%d", nums[0], nums[1], nums[2], nums[3])
	port := nums[4]*256 + nums[5]
	return host, port, nil
}
