// Package mail is the one way a message leaves the product by mail, as in
// Armature: plain text over a plain SMTP relay.
package mail

import (
	"context"
	"mime"
	"net/mail"
	"net/smtp"
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
}

func (m SMTPMailer) Send(_ context.Context, msg Mail) error {
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
	return smtp.SendMail(m.Addr, nil, sender, []string{msg.To}, []byte(text))
}
