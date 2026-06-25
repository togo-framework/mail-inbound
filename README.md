<div align="center">
  <img src=".github/assets/togo-mark.svg" alt="togo" height="64" />
  <h1>togo-framework/mail-inbound</h1>
  <p>
    <a href="https://to-go.dev/marketplace"><img src="https://img.shields.io/badge/marketplace-to--go.dev-1FC7DC" alt="marketplace" /></a>
    <a href="https://pkg.go.dev/github.com/togo-framework/mail-inbound"><img src="https://pkg.go.dev/badge/github.com/togo-framework/mail-inbound.svg" alt="pkg.go.dev" /></a>
    <img src="https://img.shields.io/badge/license-MIT-blue" alt="MIT" />
  </p>
  <p><strong>Inbound email parsing & routing for <a href="https://to-go.dev">togo</a> — turn emails into application actions.</strong></p>
</div>

## Install

```bash
togo install togo-framework/mail-inbound
```

The togo answer to **Laravel Mailbox** / **Rails Action Mailbox**. Parse raw RFC-822 messages and provider webhooks (**SendGrid Inbound Parse**, **Mailgun routes**) into a normalized `InboundMail`, then **route** each message to a handler by recipient pattern or subject regex.

## Usage

```go
mb, _ := mailinbound.FromKernel(k)

// Route support@ → open a ticket
mb.Route(mailinbound.ToPrefix("support@"), func(ctx context.Context, m mailinbound.InboundMail) error {
    return tickets.Open(m.From, m.Subject, m.Text)
})

// Route replies by plus-address token: reply+{token}@myapp.com
mb.Route(mailinbound.ToContains("reply+"), func(ctx context.Context, m mailinbound.InboundMail) error {
    if token, ok := mailinbound.PlusToken(m); ok {
        return threads.Append(token, m.Text)
    }
    return nil
})

// Match by subject
mb.Route(mailinbound.SubjectMatch(`(?i)invoice`), billingHandler)

// Anything unmatched
mb.CatchAll(func(ctx context.Context, m mailinbound.InboundMail) error { return inbox.Store(m) })
```

`InboundMail` carries `From`, `To`, `Cc`, `Subject`, `Text`, `HTML`, `MessageID`, `Headers`, and decoded `Attachments`.

## Webhooks

Point your provider's inbound parse at these endpoints (mounted automatically):

| Method | Path | Provider |
|---|---|---|
| `POST` | `/api/mail-inbound/sendgrid` | SendGrid Inbound Parse (multipart form) |
| `POST` | `/api/mail-inbound/mailgun` | Mailgun routes (form fields) |
| `POST` | `/api/mail-inbound/raw` | raw RFC-822 message |
| `GET` | `/api/mail-inbound/received` | recent inbound log |

You can also parse directly: `mailinbound.ParseRFC822(raw)`, `FromSendGrid(req)`, `FromMailgun(req)`.

## Configuration

No required env. Set your DNS MX / provider inbound-parse to forward mail to the
webhook URL, then register routes in your app. A bounded in-memory log keeps the
most recent messages.

---

<div align="center">
  <h3>Premium sponsors</h3>
  <p>
    <a href="https://id8media.com"><strong>ID8 Media</strong></a> &nbsp;·&nbsp;
    <a href="https://one-studio.co"><strong>One Studio</strong></a>
  </p>
  <p><sub>Support togo — <a href="https://github.com/sponsors/fadymondy">become a sponsor</a>.</sub></p>
</div>
