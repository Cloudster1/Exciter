package mail

import (
	"bufio"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// relay listens on a free port and hands every connection to serve.
func relay(t *testing.T, serve func(net.Conn)) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	t.Cleanup(func() {
		l.Close()
		wg.Wait()
	})
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conn.Close()
				serve(conn)
			}()
		}
	}()
	return l.Addr().String()
}

// silent accepts and then says nothing until the client hangs up, as a relay
// that has stopped answering does.
func silent(conn net.Conn) {
	_, _ = conn.Read(make([]byte, 1))
}

func sendWithin(t *testing.T, ctx context.Context, m SMTPMailer, bound time.Duration) error {
	t.Helper()
	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- m.Send(ctx, Mail{To: "reader@example.test", Subject: "Hello", Body: "Hi"}) }()
	select {
	case err := <-done:
		if took := time.Since(start); took > bound {
			t.Errorf("Send took %s, want at most %s", took, bound)
		}
		return err
	case <-time.After(bound + 5*time.Second):
		t.Fatalf("Send was still waiting on a silent relay after %s", bound+5*time.Second)
		return nil
	}
}

func TestASilentRelayRunsOutOfTime(t *testing.T) {
	m := SMTPMailer{Addr: relay(t, silent), From: "exciter@example.test", Timeout: 200 * time.Millisecond}
	err := sendWithin(t, context.Background(), m, 2*time.Second)
	if err == nil || !strings.Contains(err.Error(), "did not finish taking the mail in time") {
		t.Fatalf("Send = %v, want an error saying the relay ran out of time", err)
	}
}

// The worker's own context is the shorter bound when it is cancelled first.
func TestASendStopsWhenItsContextDoes(t *testing.T) {
	m := SMTPMailer{Addr: relay(t, silent), From: "exciter@example.test", Timeout: time.Hour}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	if err := sendWithin(t, ctx, m, 2*time.Second); err == nil {
		t.Fatal("Send returned no error for a cancelled send to a silent relay")
	}
}

// A relay that answers still gets the whole mail, now that SendMail is spelled out.
func TestAnAnsweringRelayTakesTheMail(t *testing.T) {
	got := make(chan string, 1)
	addr := relay(t, func(conn net.Conn) {
		r := bufio.NewReader(conn)
		say := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
		say("220 relay.test ready")
		var data strings.Builder
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			switch cmd := strings.ToUpper(strings.TrimSpace(line)); {
			case strings.HasPrefix(cmd, "EHLO"):
				say("250 relay.test")
			case strings.HasPrefix(cmd, "MAIL"), strings.HasPrefix(cmd, "RCPT"):
				say("250 ok")
			case cmd == "DATA":
				say("354 go ahead")
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					if l == ".\r\n" {
						break
					}
					data.WriteString(l)
				}
				got <- data.String()
				say("250 queued")
			case cmd == "QUIT":
				say("221 bye")
				return
			default:
				say("502 not here")
			}
		}
	})
	m := SMTPMailer{Addr: addr, From: "Exciter <exciter@example.test>"}
	if err := m.Send(context.Background(), Mail{To: "reader@example.test", Subject: "Hello", Body: "Hi there"}); err != nil {
		t.Fatalf("Send = %v", err)
	}
	text := <-got
	if !strings.Contains(text, "Subject: Hello\r\n") || !strings.Contains(text, "\r\n\r\nHi there") {
		t.Errorf("the relay took %q", text)
	}
}
