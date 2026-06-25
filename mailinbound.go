// Package mailinbound parses and routes inbound email for togo (Laravel Mailbox
// / Rails Action Mailbox). It parses raw RFC-822 messages and provider webhooks
// (SendGrid Inbound Parse, Mailgun routes) into a normalized InboundMail, then
// dispatches each message to the first matching handler — matched by recipient
// pattern or subject regex.
//
//	mb, _ := mailinbound.FromKernel(k)
//	mb.Route(mailinbound.ToPrefix("support@"), func(ctx context.Context, m mailinbound.InboundMail) error {
//	    return openTicket(m)
//	})
package mailinbound

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Attachment is a decoded email attachment.
type Attachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Data        []byte `json:"-"`
	Size        int    `json:"size"`
}

// InboundMail is a normalized inbound message.
type InboundMail struct {
	From        string              `json:"from"`
	To          []string            `json:"to"`
	Cc          []string            `json:"cc,omitempty"`
	Subject     string              `json:"subject"`
	Text        string              `json:"text,omitempty"`
	HTML        string              `json:"html,omitempty"`
	MessageID   string              `json:"message_id,omitempty"`
	Headers     map[string]string   `json:"headers,omitempty"`
	Attachments []Attachment        `json:"attachments,omitempty"`
	ReceivedAt  time.Time           `json:"received_at"`
}

// Matcher decides whether a handler should receive a message.
type Matcher func(m InboundMail) bool

// Handler processes an inbound message.
type Handler func(ctx context.Context, m InboundMail) error

type route struct {
	match Matcher
	fn    Handler
}

// Service is the mail-inbound runtime stored on the kernel.
type Service struct {
	mu      sync.RWMutex
	routes  []route
	catch   Handler
	log     []InboundMail
	maxLog  int
}

// togo kernel hook is registered in plugin.go to keep this file dependency-light.

func newService() *Service { return &Service{maxLog: 500} }

// Route registers a handler matched by the given Matcher (first match wins).
func (s *Service) Route(m Matcher, h Handler) {
	s.mu.Lock()
	s.routes = append(s.routes, route{m, h})
	s.mu.Unlock()
}

// CatchAll registers a fallback handler for unmatched mail.
func (s *Service) CatchAll(h Handler) {
	s.mu.Lock()
	s.catch = h
	s.mu.Unlock()
}

// Dispatch routes a message to the first matching handler (or the catch-all).
func (s *Service) Dispatch(ctx context.Context, m InboundMail) error {
	if m.ReceivedAt.IsZero() {
		m.ReceivedAt = time.Now()
	}
	s.mu.Lock()
	s.log = append(s.log, m)
	if len(s.log) > s.maxLog {
		s.log = s.log[len(s.log)-s.maxLog:]
	}
	routes := append([]route(nil), s.routes...)
	catch := s.catch
	s.mu.Unlock()

	for _, r := range routes {
		if r.match(m) {
			return r.fn(ctx, m)
		}
	}
	if catch != nil {
		return catch(ctx, m)
	}
	return nil
}

// Received returns the recent inbound log (newest last).
func (s *Service) Received() []InboundMail {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]InboundMail, len(s.log))
	copy(out, s.log)
	return out
}

// --- Matchers ---

// ToPrefix matches when any recipient starts with the given prefix (e.g. "support@").
func ToPrefix(prefix string) Matcher {
	prefix = strings.ToLower(prefix)
	return func(m InboundMail) bool {
		for _, to := range m.To {
			if strings.HasPrefix(strings.ToLower(to), prefix) {
				return true
			}
		}
		return false
	}
}

// ToContains matches when any recipient contains the substring.
func ToContains(sub string) Matcher {
	sub = strings.ToLower(sub)
	return func(m InboundMail) bool {
		for _, to := range m.To {
			if strings.Contains(strings.ToLower(to), sub) {
				return true
			}
		}
		return false
	}
}

// SubjectMatch matches when the subject matches the regular expression.
func SubjectMatch(pattern string) Matcher {
	re := regexp.MustCompile(pattern)
	return func(m InboundMail) bool { return re.MatchString(m.Subject) }
}

var plusRe = regexp.MustCompile(`^[^+@]+\+([^@]+)@`)

// PlusToken extracts the "+token" from a plus-addressed recipient
// (e.g. "reply+abc123@example.com" → "abc123", ok). Checks every recipient.
func PlusToken(m InboundMail) (string, bool) {
	for _, to := range m.To {
		if g := plusRe.FindStringSubmatch(strings.TrimSpace(to)); g != nil {
			return g[1], true
		}
	}
	return "", false
}

// --- Parsing ---

// ParseRFC822 parses a raw RFC-822 message into an InboundMail (handles
// multipart bodies + attachments).
func ParseRFC822(raw []byte) (InboundMail, error) {
	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		return InboundMail{}, fmt.Errorf("mail-inbound: parse: %w", err)
	}
	out := InboundMail{
		From:      headerAddr(msg.Header.Get("From")),
		To:        headerAddrs(msg.Header.Get("To")),
		Cc:        headerAddrs(msg.Header.Get("Cc")),
		Subject:   decodeHeader(msg.Header.Get("Subject")),
		MessageID: strings.Trim(msg.Header.Get("Message-Id"), "<>"),
		Headers:   map[string]string{},
		ReceivedAt: time.Now(),
	}
	for k := range msg.Header {
		out.Headers[k] = msg.Header.Get(k)
	}

	ctype := msg.Header.Get("Content-Type")
	mediaType, params, _ := mime.ParseMediaType(ctype)
	if strings.HasPrefix(mediaType, "multipart/") {
		if err := parseMultipart(msg.Body, params["boundary"], &out); err != nil {
			return out, err
		}
	} else {
		body, _ := io.ReadAll(msg.Body)
		if strings.HasPrefix(mediaType, "text/html") {
			out.HTML = string(body)
		} else {
			out.Text = string(body)
		}
	}
	return out, nil
}

func parseMultipart(body io.Reader, boundary string, out *InboundMail) error {
	if boundary == "" {
		b, _ := io.ReadAll(body)
		out.Text = string(b)
		return nil
	}
	mr := multipart.NewReader(body, boundary)
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("mail-inbound: multipart: %w", err)
		}
		data, _ := io.ReadAll(part)
		ct, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		filename := part.FileName()
		switch {
		case filename != "":
			out.Attachments = append(out.Attachments, Attachment{
				Filename: filename, ContentType: ct, Data: data, Size: len(data),
			})
		case strings.HasPrefix(ct, "text/html"):
			out.HTML = string(data)
		case strings.HasPrefix(ct, "text/plain"), ct == "":
			out.Text = string(data)
		}
	}
	return nil
}

func headerAddr(v string) string {
	if a, err := mail.ParseAddress(v); err == nil {
		return a.Address
	}
	return strings.TrimSpace(v)
}

func headerAddrs(v string) []string {
	if v == "" {
		return nil
	}
	if list, err := mail.ParseAddressList(v); err == nil {
		out := make([]string, len(list))
		for i, a := range list {
			out[i] = a.Address
		}
		return out
	}
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func decodeHeader(v string) string {
	dec := new(mime.WordDecoder)
	if s, err := dec.DecodeHeader(v); err == nil {
		return s
	}
	return v
}

func splitAddrs(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, headerAddr(p))
		}
	}
	return out
}

// FromSendGrid normalizes a SendGrid Inbound Parse (multipart/form-data) request.
func FromSendGrid(r *http.Request) (InboundMail, error) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		// also accept urlencoded
		_ = r.ParseForm()
	}
	m := InboundMail{
		From:       headerAddr(r.FormValue("from")),
		To:         splitAddrs(r.FormValue("to")),
		Cc:         splitAddrs(r.FormValue("cc")),
		Subject:    r.FormValue("subject"),
		Text:       r.FormValue("text"),
		HTML:       r.FormValue("html"),
		ReceivedAt: time.Now(),
	}
	if r.MultipartForm != nil {
		for _, fhs := range r.MultipartForm.File {
			for _, fh := range fhs {
				f, err := fh.Open()
				if err != nil {
					continue
				}
				data, _ := io.ReadAll(f)
				f.Close()
				m.Attachments = append(m.Attachments, Attachment{
					Filename: fh.Filename, ContentType: fh.Header.Get("Content-Type"),
					Data: data, Size: len(data),
				})
			}
		}
	}
	return m, nil
}

// FromMailgun normalizes a Mailgun route (form fields) request.
func FromMailgun(r *http.Request) (InboundMail, error) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		_ = r.ParseForm()
	}
	get := func(keys ...string) string {
		for _, k := range keys {
			if v := r.FormValue(k); v != "" {
				return v
			}
		}
		return ""
	}
	return InboundMail{
		From:       headerAddr(get("sender", "from")),
		To:         splitAddrs(get("recipient", "to")),
		Cc:         splitAddrs(get("Cc", "cc")),
		Subject:    get("subject", "Subject"),
		Text:       get("body-plain", "stripped-text"),
		HTML:       get("body-html", "stripped-html"),
		MessageID:  strings.Trim(get("Message-Id", "message-id"), "<>"),
		ReceivedAt: time.Now(),
	}, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
