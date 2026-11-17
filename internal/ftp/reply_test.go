package ftp

import (
	"bufio"
	"strings"
	"testing"
)

func TestReadReplySingle(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("220 hello there\r\n"))
	rep, err := readReply(r)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Code != 220 || rep.Message != "hello there" {
		t.Fatalf("got %+v", rep)
	}
}

func TestReadReplyMultiline(t *testing.T) {
	body := "220-Welcome\r\nsecond line\r\n220 done\r\n"
	r := bufio.NewReader(strings.NewReader(body))
	rep, err := readReply(r)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Code != 220 {
		t.Fatalf("code = %d", rep.Code)
	}
	if !strings.Contains(rep.Message, "Welcome") || !strings.Contains(rep.Message, "done") {
		t.Fatalf("message = %q", rep.Message)
	}
}

func TestParseEPSV(t *testing.T) {
	port, err := parseEPSV("Entering Extended Passive Mode (|||6446|)")
	if err != nil || port != 6446 {
		t.Fatalf("port = %d err = %v", port, err)
	}
	if _, err := parseEPSV("garbage"); err == nil {
		t.Fatal("want error")
	}
}

func TestParsePASV(t *testing.T) {
	host, port, err := parsePASV("Entering Passive Mode (127,0,0,1,25,84)")
	if err != nil {
		t.Fatal(err)
	}
	if host != "127.0.0.1" || port != 25*256+84 {
		t.Fatalf("host = %s port = %d", host, port)
	}
	if _, _, err := parsePASV("(1,2,3)"); err == nil {
		t.Fatal("want error")
	}
}
