package nexus

import (
	"io"
	"net/http"
	"strings"
)

const webhookBodyLimit = 1 << 20 // 1 MiB

// WebhookHandler ingests POST bodies into webhook channels. Mount it at
// pattern "POST /ingest/{name}"; it reads the channel from r.PathValue.
func (n *Nexus) WebhookHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		ch, ok := n.channels[name]
		if !ok || ch.cfg.Type != TypeWebhook {
			http.Error(w, "unknown webhook channel", http.StatusNotFound)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, webhookBodyLimit+1))
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		if int64(len(body)) > webhookBodyLimit {
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
			return
		}
		text := strings.TrimSpace(string(body))
		if text == "" {
			http.Error(w, "empty body", http.StatusBadRequest)
			return
		}

		ch.Ingest(text)
		w.WriteHeader(http.StatusAccepted)
	})
}
