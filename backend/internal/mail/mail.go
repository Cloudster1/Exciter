// Package mail is the one way a message leaves the product by mail, as in
// Armature: plain text over a plain SMTP relay.
package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"strings"
	"time"
)

// Mail is one message to one address.
type Mail struct {
	To      string
	Subject string
	Body    string
}

// headerValue keeps a header on one line: a newline in a requirement title or a
// name would start a header of its writer's choosing.
func headerValue(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r < 0x20 {
			return ' '
		}
		return r
	}, s)
}

// encodeSubject writes a subject beyond ASCII as a header may carry it.
func encodeSubject(s string) string {
	return mime.QEncoding.Encode("UTF-8", s)
}

// Mailer sends one message. The SMTP mailer is the real one; tests keep what
// would have been sent.
type Mailer interface {
	Send(ctx context.Context, m Mail) error
}

// SMTPMailer sends through a plain SMTP relay, which in development is
// Mailpit and in production whatever the operator points it at.
type SMTPMailer struct {
	Addr string
	From string
	// Timeout bounds one whole send, dial included; zero is DefaultTimeout.
	Timeout time.Duration
}

// DefaultTimeout is long enough for a slow relay and short enough that a hung
// one does not hold back the mails queued behind it.
const DefaultTimeout = 30 * time.Second

func (m SMTPMailer) Send(ctx context.Context, msg Mail) error {
	// The envelope sender is the bare address; the display name belongs in
	// the header only, and a relay refuses it anywhere else.
	sender := m.From
	if parsed, err := mail.ParseAddress(m.From); err == nil {
		sender = parsed.Address
	}
	headers := []string{
		"From: " + headerValue(m.From),
		"To: " + headerValue(msg.To),
		"Subject: " + encodeSubject(headerValue(msg.Subject)),
		"Date: " + time.Now().UTC().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
		// Says this is a machine's mail, so auto-responders leave it alone.
		"Auto-Submitted: auto-generated",
	}
	body := strings.ReplaceAll(strings.ReplaceAll(msg.Body, "\r\n", "\n"), "\n", "\r\n")
	text := strings.Join(append(headers, "", body), "\r\n")
	return m.deliver(ctx, sender, msg.To, []byte(text))
}

// deliver is smtp.SendMail with a deadline: SendMail neither dials with a
// timeout nor ever gives up on a relay that stops answering.
func (m SMTPMailer) deliver(ctx context.Context, sender, to string, text []byte) error {
	timeout := m.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	err := m.converse(ctx, sender, to, text)
	// The connection's deadline and the context's timer are the same instant
	// but not the same clock, so either may be the one that reports it.
	if err != nil && (ctx.Err() != nil || errors.Is(err, os.ErrDeadlineExceeded)) {
		return fmt.Errorf("The mail server at %s did not finish taking the mail in time, so it was not sent; check that the relay is up and answering: %w", m.Addr, errors.Join(ctx.Err(), err))
	}
	return err
}

func (m SMTPMailer) converse(ctx context.Context, sender, to string, text []byte) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", m.Addr)
	if err != nil {
		return err
	}
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		conn.Close()
		return err
	}
	// The deadline covers a relay that goes quiet; this covers the caller
	// giving up before it is reached.
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	host, _, _ := net.SplitHostPort(m.Addr)
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}
	if err := c.Mail(sender); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(text); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
