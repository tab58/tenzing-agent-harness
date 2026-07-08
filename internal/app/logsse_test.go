package app

import (
	"bufio"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLogBroadcasterWrite(t *testing.T) {
	b := NewLogBroadcaster()
	// no subscribers: must not block or error
	n, err := b.Write([]byte("hello\n"))
	if err != nil || n != 6 {
		t.Fatalf("Write = (%d, %v), want (6, nil)", n, err)
	}
}

func TestLogBroadcasterSSE(t *testing.T) {
	b := NewLogBroadcaster()
	srv := httptest.NewServer(b.SSEHandler())
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}

	// give the handler a moment to register the subscriber
	time.Sleep(50 * time.Millisecond)
	b.Write([]byte("level=INFO msg=test\n"))

	lineCh := make(chan string, 10)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lineCh <- sc.Text()
		}
	}()

	var event, data string
	deadline := time.After(2 * time.Second)
	for event == "" || data == "" {
		select {
		case l := <-lineCh:
			if strings.HasPrefix(l, "event: ") {
				event = strings.TrimPrefix(l, "event: ")
			}
			if strings.HasPrefix(l, "data: ") {
				data = strings.TrimPrefix(l, "data: ")
			}
		case <-deadline:
			t.Fatalf("timed out; event=%q data=%q", event, data)
		}
	}
	if event != "log" {
		t.Errorf("event = %q, want log", event)
	}
	if !strings.Contains(data, "msg=test") {
		t.Errorf("data = %q, want to contain msg=test", data)
	}
}
