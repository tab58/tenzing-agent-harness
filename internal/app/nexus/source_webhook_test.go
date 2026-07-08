package nexus

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func webhookMux(n *Nexus) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("POST /ingest/{name}", n.WebhookHandler())
	return mux
}

func TestWebhookIngest(t *testing.T) {
	n := newTestNexus(t, []ChannelConfig{{Name: "hooks", Type: TypeWebhook}}, nil, nil)
	srv := httptest.NewServer(webhookMux(n))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/ingest/hooks", "text/plain", strings.NewReader("payload-1\n"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status = %d, want 202", resp.StatusCode)
	}

	entries, err := n.Read("hooks", 10, false)
	if err != nil || len(entries) != 1 || entries[0].Text != "payload-1" {
		t.Errorf("entries = %v (err %v), want [payload-1]", entries, err)
	}
}

func TestWebhookUnknownChannel(t *testing.T) {
	n := newTestNexus(t, []ChannelConfig{{Name: "hooks", Type: TypeWebhook}}, nil, nil)
	srv := httptest.NewServer(webhookMux(n))
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/ingest/nope", "text/plain", strings.NewReader("x"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestWebhookWrongChannelType(t *testing.T) {
	n := newTestNexus(t, []ChannelConfig{{Name: "f", Type: TypeFileTail, Path: "/tmp/x"}}, nil, nil)
	srv := httptest.NewServer(webhookMux(n))
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/ingest/f", "text/plain", strings.NewReader("x"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for non-webhook channel", resp.StatusCode)
	}
}

func TestWebhookBodyTooLarge(t *testing.T) {
	n := newTestNexus(t, []ChannelConfig{{Name: "hooks", Type: TypeWebhook}}, nil, nil)
	srv := httptest.NewServer(webhookMux(n))
	defer srv.Close()

	big := strings.Repeat("x", 2<<20) // 2 MiB > 1 MiB cap
	resp, _ := http.Post(srv.URL+"/ingest/hooks", "text/plain", strings.NewReader(big))
	resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", resp.StatusCode)
	}
}

func TestWebhookEmptyBody(t *testing.T) {
	n := newTestNexus(t, []ChannelConfig{{Name: "hooks", Type: TypeWebhook}}, nil, nil)
	srv := httptest.NewServer(webhookMux(n))
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/ingest/hooks", "text/plain", strings.NewReader("  \n"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for empty body", resp.StatusCode)
	}
}
