package mailinbound

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

const multipartMsg = "From: Alice <alice@example.com>\r\n" +
	"To: support@myapp.com\r\n" +
	"Subject: Help please\r\n" +
	"Content-Type: multipart/mixed; boundary=BOUND\r\n" +
	"\r\n" +
	"--BOUND\r\n" +
	"Content-Type: text/plain\r\n\r\n" +
	"My printer is broken.\r\n" +
	"--BOUND\r\n" +
	"Content-Type: text/plain; name=\"log.txt\"\r\n" +
	"Content-Disposition: attachment; filename=\"log.txt\"\r\n\r\n" +
	"error 42\r\n" +
	"--BOUND--\r\n"

func TestParseRFC822(t *testing.T) {
	m, err := ParseRFC822([]byte(multipartMsg))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m.From != "alice@example.com" {
		t.Errorf("from = %q", m.From)
	}
	if len(m.To) != 1 || m.To[0] != "support@myapp.com" {
		t.Errorf("to = %v", m.To)
	}
	if m.Subject != "Help please" {
		t.Errorf("subject = %q", m.Subject)
	}
	if !strings.Contains(m.Text, "printer is broken") {
		t.Errorf("text = %q", m.Text)
	}
	if len(m.Attachments) != 1 || m.Attachments[0].Filename != "log.txt" || !strings.Contains(string(m.Attachments[0].Data), "error 42") {
		t.Errorf("attachments = %+v", m.Attachments)
	}
}

func TestPlusToken(t *testing.T) {
	m := InboundMail{To: []string{"reply+ticket-123@myapp.com"}}
	tok, ok := PlusToken(m)
	if !ok || tok != "ticket-123" {
		t.Fatalf("PlusToken = %q, %v", tok, ok)
	}
	if _, ok := PlusToken(InboundMail{To: []string{"plain@x.com"}}); ok {
		t.Fatal("plain address should have no token")
	}
}

func TestRoutingFirstMatchAndCatchAll(t *testing.T) {
	s := newService()
	var hit string
	s.Route(ToPrefix("support@"), func(ctx context.Context, m InboundMail) error { hit = "support"; return nil })
	s.Route(SubjectMatch(`(?i)invoice`), func(ctx context.Context, m InboundMail) error { hit = "billing"; return nil })
	s.CatchAll(func(ctx context.Context, m InboundMail) error { hit = "catchall"; return nil })

	// by recipient prefix
	hit = ""
	_ = s.Dispatch(context.Background(), InboundMail{To: []string{"support@myapp.com"}, Subject: "hi"})
	if hit != "support" {
		t.Errorf("to-prefix route = %q", hit)
	}
	// by subject regex
	hit = ""
	_ = s.Dispatch(context.Background(), InboundMail{To: []string{"hello@myapp.com"}, Subject: "Your Invoice #9"})
	if hit != "billing" {
		t.Errorf("subject route = %q", hit)
	}
	// falls through to catch-all
	hit = ""
	_ = s.Dispatch(context.Background(), InboundMail{To: []string{"random@myapp.com"}, Subject: "hello"})
	if hit != "catchall" {
		t.Errorf("catch-all = %q", hit)
	}
	// logged
	if len(s.Received()) != 3 {
		t.Errorf("received log = %d, want 3", len(s.Received()))
	}
}

func TestFromSendGrid(t *testing.T) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	w.WriteField("from", "Bob <bob@example.com>")
	w.WriteField("to", "support@myapp.com")
	w.WriteField("subject", "Need help")
	w.WriteField("text", "please assist")
	// an attachment
	fw, _ := w.CreateFormFile("attachment1", "note.txt")
	fw.Write([]byte("hello file"))
	w.Close()

	req := httptest.NewRequest("POST", "/api/mail-inbound/sendgrid", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())

	m, err := FromSendGrid(req)
	if err != nil {
		t.Fatal(err)
	}
	if m.From != "bob@example.com" || m.Subject != "Need help" || m.Text != "please assist" {
		t.Errorf("sendgrid parse = %+v", m)
	}
	if len(m.To) != 1 || m.To[0] != "support@myapp.com" {
		t.Errorf("to = %v", m.To)
	}
	if len(m.Attachments) != 1 || m.Attachments[0].Filename != "note.txt" {
		t.Errorf("attachments = %+v", m.Attachments)
	}
}

func TestFromMailgun(t *testing.T) {
	form := strings.NewReader("sender=carol%40example.com&recipient=support%40myapp.com&subject=Hi&body-plain=hello+there")
	req := httptest.NewRequest("POST", "/api/mail-inbound/mailgun", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	m, err := FromMailgun(req)
	if err != nil {
		t.Fatal(err)
	}
	if m.From != "carol@example.com" || m.To[0] != "support@myapp.com" || m.Subject != "Hi" || m.Text != "hello there" {
		t.Errorf("mailgun parse = %+v", m)
	}
}

func TestServeHTTPRawEndpoint(t *testing.T) {
	s := newService()
	var got InboundMail
	s.CatchAll(func(ctx context.Context, m InboundMail) error { got = m; return nil })
	r := chiRouterFor(s)
	req := httptest.NewRequest("POST", "/api/mail-inbound/raw", strings.NewReader(multipartMsg))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	if got.Subject != "Help please" {
		t.Errorf("dispatched mail subject = %q", got.Subject)
	}
}

// chiRouterFor mounts the service routes on a fresh router for testing.
func chiRouterFor(s *Service) http.Handler {
	r := chi.NewRouter()
	s.mountRoutes(r)
	return r
}
