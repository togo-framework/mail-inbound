# mail-inbound — usage

## Route inbound mail
```go
mb, _ := mailinbound.FromKernel(k)
mb.Route(mailinbound.ToPrefix("support@"), supportHandler)     // by recipient prefix
mb.Route(mailinbound.ToContains("reply+"), replyHandler)        // plus-addressing
mb.Route(mailinbound.SubjectMatch(`(?i)invoice`), billing)      // by subject regex
mb.CatchAll(fallback)                                           // everything else
```
First matching route wins. Handlers receive a normalized `InboundMail` (From/To/Cc/Subject/Text/HTML/Headers/Attachments).

## Plus-address tokens
```go
token, ok := mailinbound.PlusToken(m) // reply+abc123@app.com → "abc123"
```

## Provider webhooks
Mounted automatically on the kernel router:
- `POST /api/mail-inbound/sendgrid` — SendGrid Inbound Parse (multipart)
- `POST /api/mail-inbound/mailgun` — Mailgun route (form fields)
- `POST /api/mail-inbound/raw` — raw RFC-822
- `GET  /api/mail-inbound/received` — recent inbound log

Set your provider's inbound parse / MX to forward to the webhook URL.

## Parse directly
```go
m, _ := mailinbound.ParseRFC822(raw)   // multipart bodies + attachments
m, _ := mailinbound.FromSendGrid(req)
m, _ := mailinbound.FromMailgun(req)
```
