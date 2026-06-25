package mailinbound

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/togo-framework/togo"
)

func init() {
	togo.RegisterProviderFunc("mail-inbound", togo.PriorityService, func(k *togo.Kernel) error {
		s := newService()
		k.Set("mail-inbound", s)
		if k.Router != nil {
			s.mountRoutes(k.Router)
		}
		return nil
	})
}

// FromKernel returns the mail-inbound Service.
func FromKernel(k *togo.Kernel) (*Service, bool) {
	v, ok := k.Get("mail-inbound")
	if !ok {
		return nil, false
	}
	s, ok := v.(*Service)
	return s, ok
}

func (s *Service) mountRoutes(r chi.Router) {
	r.Route("/api/mail-inbound", func(r chi.Router) {
		r.Post("/sendgrid", func(w http.ResponseWriter, req *http.Request) {
			m, err := FromSendGrid(req)
			if err != nil {
				writeJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
			_ = s.Dispatch(req.Context(), m)
			writeJSON(w, 200, map[string]bool{"ok": true})
		})
		r.Post("/mailgun", func(w http.ResponseWriter, req *http.Request) {
			m, err := FromMailgun(req)
			if err != nil {
				writeJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
			_ = s.Dispatch(req.Context(), m)
			writeJSON(w, 200, map[string]bool{"ok": true})
		})
		r.Post("/raw", func(w http.ResponseWriter, req *http.Request) {
			body, _ := readAll(req)
			m, err := ParseRFC822(body)
			if err != nil {
				writeJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
			_ = s.Dispatch(req.Context(), m)
			writeJSON(w, 200, map[string]bool{"ok": true})
		})
		r.Get("/received", func(w http.ResponseWriter, req *http.Request) {
			writeJSON(w, 200, s.Received())
		})
	})
}

func readAll(req *http.Request) ([]byte, error) {
	defer req.Body.Close()
	const max = 32 << 20
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for len(buf) < max {
		n, err := req.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
	}
	return buf, nil
}
