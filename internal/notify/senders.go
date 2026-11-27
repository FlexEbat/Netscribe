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
		return webhook{url: string(ch.URL), format: ch.Format, client: newClient(requestTimeout)}
	case config.ChannelNtfy:
		return ntfy{url: string(ch.URL), topic: string(ch.Topic), token: string(ch.Token), client: newClient(requestTimeout)}
	case config.ChannelGotify:
		return gotify{url: string(ch.URL), token: string(ch.Token), client: newClient(requestTimeout)}
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
func post(ctx context.Context, client *http.Client, url string, payload any, headers ...[2]string) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return errors.New("the message could not be encoded")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return errors.New("the address is not usable")
	}
	req.Header.Set("Content-Type", "application/json")
	for _, h := range headers {
		req.Header.Set(h[0], h[1])
	}
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
	format string // "" and json are the body of old
	client *http.Client
}

const (
	discordRunes = 1900 // Discord refuses a content of more than 2000 characters
	chatRunes    = 3500 // Slack and Teams take more; this keeps one message readable
)

func (w webhook) Send(ctx context.Context, subject, body string) error {
	return post(ctx, w.client, w.url, w.payload(subject, body))
}

// payload is the body in the shape the service expects. Each chat format is one text field,
// with the subject on its own line, and no markup: the text holds names from the network.
func (w webhook) payload(subject, body string) any {
	text := subject + "\n" + body
	switch w.format {
	case config.FormatSlack:
		return map[string]string{"text": cut(text, chatRunes)}
	case config.FormatTeams:
		return map[string]string{"text": cut(text, chatRunes)}
	case config.FormatDiscord:
		return map[string]string{"content": cut(text, discordRunes)}
	}
	return map[string]string{"subject": subject, "text": body}
}

// cut keeps the first n runes.
func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// ntfyBytes is the largest message ntfy takes, 4096 bytes, with room to spare.
const ntfyBytes = 4000

type ntfy struct {
	url, topic, token string
	client            *http.Client
}

// Send publishes with the JSON form of the ntfy API: the topic is in the body, so it is in
// no URL that an error or a log could show.
func (n ntfy) Send(ctx context.Context, subject, body string) error {
	payload := map[string]string{"topic": n.topic, "title": subject, "message": cutBytes(body, ntfyBytes)}
	if n.token != "" {
		return post(ctx, n.client, n.url, payload, [2]string{"Authorization", "Bearer " + n.token})
	}
	return post(ctx, n.client, n.url, payload)
}

// cutBytes keeps at most n bytes and never ends in the middle of a character.
func cutBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

type gotify struct {
	url, token string
	client     *http.Client
}

// Send posts to /message with the key in a header and not in the query, which would put it
// in the URL of every error.
func (g gotify) Send(ctx context.Context, subject, body string) error {
	return post(ctx, g.client, strings.TrimRight(g.url, "/")+"/message",
		map[string]any{"title": subject, "message": body, "priority": 5},
		[2]string{"X-Gotify-Key", g.token})
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
