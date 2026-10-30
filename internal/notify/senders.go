package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/FlexEbat/Netscribe/internal/config"
)

const (
	requestTimeout = 5 * time.Second
	mailTimeout    = 15 * time.Second
	maxAnswer      = 4 << 10 // the part of an answer that is read
	telegramRunes  = 4000
)

func newSender(ch config.ChannelConfig) sender {
	switch ch.Type {
	case config.ChannelWebhook:
		return webhook{url: string(ch.URL), client: newClient(requestTimeout)}
	case config.ChannelTelegram:
		return telegram{base: "https://api.telegram.org", token: string(ch.BotToken), chat: string(ch.ChatID), client: newClient(requestTimeout)}
	default:
		return email{host: ch.Host, port: ch.Port, user: ch.User, password: string(ch.Password), from: ch.From, to: ch.To,
			dial: (&net.Dialer{Timeout: requestTimeout}).DialContext}
	}
}

// newClient never follows a redirect: an address that a person typed is the only one that
// gets a message.
func newClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:       timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// classify turns a transport error into words that hold no address. The errors of net/http
// carry the whole URL, and for Telegram the URL holds the token.
func classify(err error) error {
	var ne net.Error
	switch {
	case errors.Is(err, context.Canceled):
		return errors.New("canceled")
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return errors.New("timeout")
	}
	return errors.New("connection failed")
}

// post sends a JSON body and reads at most maxAnswer bytes of the answer.
func post(ctx context.Context, client *http.Client, url string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return errors.New("the message could not be encoded")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return errors.New("the address is not usable")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return classify(err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxAnswer))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("the server answered %d", resp.StatusCode)
	}
	return nil
}

type webhook struct {
	url    string
	client *http.Client
}

func (w webhook) Send(ctx context.Context, subject, body string) error {
	return post(ctx, w.client, w.url, map[string]string{"subject": subject, "text": body})
}

type telegram struct {
	base, token, chat string
	client            *http.Client
}

func (t telegram) Send(ctx context.Context, subject, body string) error {
	text := subject + "\n" + body
	if utf8.RuneCountInString(text) > telegramRunes {
		text = string([]rune(text)[:telegramRunes])
	}
	// No parse mode: the text holds names that came from the network.
	return post(ctx, t.client, t.base+"/bot"+t.token+"/sendMessage", map[string]string{"chat_id": t.chat, "text": text})
}

type email struct {
	host, user, password, from string
	port                       int
	to                         []string
	dial                       func(ctx context.Context, network, addr string) (net.Conn, error)
}

func (e email) Send(ctx context.Context, subject, body string) error {
	from, err := mail.ParseAddress(e.from)
	if err != nil {
		return errors.New("the sender address is not usable")
	}
	rcpt := make([]*mail.Address, 0, len(e.to))
	for _, a := range e.to {
		p, err := mail.ParseAddress(a)
		if err != nil {
			return errors.New("a recipient address is not usable")
		}
		rcpt = append(rcpt, p)
	}

	conn, err := e.dial(ctx, "tcp", net.JoinHostPort(e.host, strconv.Itoa(e.port)))
	if err != nil {
		return classify(err)
	}
	_ = conn.SetDeadline(time.Now().Add(mailTimeout))
	c, err := smtp.NewClient(conn, e.host)
	if err != nil {
		_ = conn.Close()
		return errors.New("the mail server did not greet")
	}
	defer func() { _ = c.Close() }()

	// The password only travels inside TLS, and the certificate must be the server's.
	if ok, _ := c.Extension("STARTTLS"); !ok {
		return errors.New("the mail server does not offer STARTTLS")
	}
	if err := c.StartTLS(&tls.Config{ServerName: e.host, MinVersion: tls.VersionTLS12}); err != nil {
		return errors.New("TLS to the mail server failed")
	}
	if e.user != "" {
		if err := c.Auth(smtp.PlainAuth("", e.user, e.password, e.host)); err != nil {
			return errors.New("the mail server refused the login")
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return errors.New("the mail server refused the sender")
	}
	for _, r := range rcpt {
		if err := c.Rcpt(r.Address); err != nil {
			return errors.New("the mail server refused a recipient")
		}
	}
	w, err := c.Data()
	if err != nil {
		return errors.New("the mail server refused the message")
	}
	if _, err := io.WriteString(w, message(from, rcpt, subject, body)); err != nil {
		return errors.New("the message could not be written")
	}
	if err := w.Close(); err != nil {
		return errors.New("the mail server refused the message")
	}
	_ = c.Quit()
	return nil
}

func message(from *mail.Address, to []*mail.Address, subject, body string) string {
	list := make([]string, 0, len(to))
	for _, a := range to {
		list = append(list, a.String())
	}
	var b strings.Builder
	b.WriteString("From: " + from.String() + "\r\n")
	b.WriteString("To: " + strings.Join(list, ", ") + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", strings.NewReplacer("\r", " ", "\n", " ").Replace(subject)) + "\r\n")
	b.WriteString("Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n") + "\r\n")
	return b.String()
}
