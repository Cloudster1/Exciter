package mail

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

// conversation is what a relay was told: the envelope, and the message as it came over the wire and as it reads
// once the relay has taken back the dots a client doubles.
type conversation struct {
	from, to  string
	wire      string
	unstuffed string
}

// recordingRelay answers as a plain relay without STARTTLS does and hands over what each connection said.
func recordingRelay(t *testing.T) (string, <-chan conversation) {
	t.Helper()
	got := make(chan conversation, 1)
	addr := relay(t, func(conn net.Conn) {
		r := bufio.NewReader(conn)
		say := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
		say("220 relay.test ready")
		var c conversation
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.TrimRight(line, "\r\n")
			switch upper := strings.ToUpper(cmd); {
			case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
				say("250 relay.test")
			case strings.HasPrefix(upper, "MAIL FROM:"):
				c.from = cmd[len("MAIL FROM:"):]
				say("250 ok")
			case strings.HasPrefix(upper, "RCPT TO:"):
				c.to = cmd[len("RCPT TO:"):]
				say("250 ok")
			case upper == "DATA":
				say("354 go ahead")
				var wire, plain strings.Builder
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					if l == ".\r\n" {
						break
					}
					wire.WriteString(l)
					plain.WriteString(strings.TrimPrefix(l, "."))
				}
				c.wire, c.unstuffed = wire.String(), plain.String()
				got <- c
				say("250 queued")
			case upper == "QUIT":
				say("221 bye")
				return
			default:
				say("502 not here")
			}
		}
	})
	return addr, got
}

func TestTheRelayGetsTheMailAsWritten(t *testing.T) {
	addr, got := recordingRelay(t)
	m := SMTPMailer{Addr: addr, From: "Exciter <no-reply@exciter.example.com>", Timeout: 5 * time.Second}
	body := "Alice mentioned you.\n.hidden starts with a dot\n..and this with two\nThe end."
	if err := m.Send(context.Background(), Mail{To: "bob@example.test", Subject: "You were mentioned", Body: body}); err != nil {
		t.Fatalf("Send = %v", err)
	}
	var c conversation
	select {
	case c = <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("the relay was never given the mail")
	}

	if c.from != "<no-reply@exciter.example.com>" {
		t.Errorf("MAIL FROM was %q, want the bare address of the sender", c.from)
	}
	if c.to != "<bob@example.test>" {
		t.Errorf("RCPT TO was %q, want the reader", c.to)
	}
	head, text, ok := strings.Cut(c.unstuffed, "\r\n\r\n")
	if !ok {
		t.Fatalf("the message has no blank line between its headers and its body:\n%q", c.unstuffed)
	}
	headers := strings.Split(head, "\r\n")
	want := []string{
		"From: Exciter <no-reply@exciter.example.com>",
		"To: bob@example.test",
		"Subject: You were mentioned",
		"Date: ",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
		"Auto-Submitted: auto-generated",
	}
	if len(headers) != len(want) {
		t.Fatalf("the headers were %q, want %d of them", headers, len(want))
	}
	for i, w := range want {
		if !strings.HasPrefix(headers[i], w) {
			t.Errorf("header %d is %q, want it to start %q", i+1, headers[i], w)
		}
	}
	date := strings.TrimPrefix(headers[3], "Date: ")
	if _, err := time.Parse(time.RFC1123Z, date); err != nil {
		t.Errorf("the Date header %q is not RFC 1123 with a zone: %v", date, err)
	}
	// Every line ends in CRLF, and a line starting with a dot arrives whole once the relay takes back the doubled dot.
	if wantText := strings.ReplaceAll(body, "\n", "\r\n") + "\r\n"; text != wantText {
		t.Errorf("the body arrived as %q, want %q", text, wantText)
	}
	if !strings.Contains(c.wire, "\r\n..hidden starts with a dot\r\n") || !strings.Contains(c.wire, "\r\n...and this with two\r\n") {
		t.Errorf("a line starting with a dot went over the wire undoubled, which ends the message there:\n%q", c.wire)
	}
}

// A body already in CRLF is not doubled to CR CR LF.
func TestABodyInCRLFStaysCRLF(t *testing.T) {
	addr, got := recordingRelay(t)
	m := SMTPMailer{Addr: addr, From: "no-reply@exciter.example.com", Timeout: 5 * time.Second}
	if err := m.Send(context.Background(), Mail{To: "bob@example.test", Subject: "Hi", Body: "one\r\ntwo"}); err != nil {
		t.Fatalf("Send = %v", err)
	}
	c := <-got
	if c.from != "<no-reply@exciter.example.com>" {
		t.Errorf("MAIL FROM was %q for a sender with no display name", c.from)
	}
	if _, text, _ := strings.Cut(c.unstuffed, "\r\n\r\n"); text != "one\r\ntwo\r\n" {
		t.Errorf("the body arrived as %q, want one CRLF between the lines", text)
	}
}
