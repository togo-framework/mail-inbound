---
name: mail-inbound
description: Receive and route inbound email in a togo app with the mail-inbound plugin — parse RFC-822 / SendGrid / Mailgun, route by recipient or subject, extract plus-address tokens, handle attachments.
---

# togo mail-inbound

Use this skill to turn incoming email into application actions.

## Register routes
```go
mb, _ := mailinbound.FromKernel(k)
mb.Route(mailinbound.ToPrefix("support@"), func(ctx context.Context, m mailinbound.InboundMail) error { /* open ticket */ return nil })
mb.Route(mailinbound.SubjectMatch(`(?i)invoice`), billingHandler)
mb.CatchAll(fallback)
```
First match wins; `m` has From/To/Cc/Subject/Text/HTML/Headers/Attachments.

## Plus-addressing (threaded replies)
`reply+{token}@app.com` → `token, ok := mailinbound.PlusToken(m)`.

## Wire a provider
Point SendGrid Inbound Parse / Mailgun route / MX at:
- `POST /api/mail-inbound/sendgrid`
- `POST /api/mail-inbound/mailgun`
- `POST /api/mail-inbound/raw` (raw RFC-822)

## Notes
- Validate the sender / verify the provider signature before acting on mail (anti-spoofing).
- Attachments are decoded into `m.Attachments` — store via the `media`/`storage` plugin.
