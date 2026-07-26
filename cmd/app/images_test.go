package main

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tab58/huma-http-server/router"
)

func TestValidateImages(t *testing.T) {
	valid := base64.StdEncoding.EncodeToString([]byte("png-bytes"))
	tests := []struct {
		name    string
		in      []imageInput
		wantErr string
	}{
		{"nil ok", nil, ""},
		{"valid", []imageInput{{MediaType: "image/png", Data: valid}}, ""},
		{"bad media type", []imageInput{{MediaType: "text/html", Data: valid}}, "not an image MIME type"},
		{"empty data", []imageInput{{MediaType: "image/png", Data: ""}}, "empty data"},
		{"bad base64", []imageInput{{MediaType: "image/png", Data: "not!!base64"}}, "not valid base64"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := validateImages(tt.in)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateImages: %v", err)
			}
			if len(out) != len(tt.in) {
				t.Errorf("got %d images, want %d", len(out), len(tt.in))
			}
		})
	}
}

// The test server's model has no vision support: image-bearing queries get a
// clean 400 before any turn starts.
func TestHandleQueryRejectsImagesOnNonVisionModel(t *testing.T) {
	agent := &gatedAgent{gate: make(chan struct{})}
	api := newTestServer(t, agent)

	in := &queryInput{}
	in.Body.Query = "what is this?"
	in.Body.Images = []imageInput{{MediaType: "image/png", Data: base64.StdEncoding.EncodeToString([]byte("x"))}}
	_, err := api.handleQuery(context.Background(), router.MapAuthInfo{}, in)
	if err == nil || !strings.Contains(err.Error(), "does not support image input") {
		t.Fatalf("err = %v, want vision-capability 400", err)
	}
	if len(agent.seen()) != 0 {
		t.Error("turn started despite capability rejection")
	}
}

func TestExtractImageArgs(t *testing.T) {
	dir := t.TempDir()
	pngPath := filepath.Join(dir, "shot.PNG")
	if err := os.WriteFile(pngPath, []byte("png-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("mixed args", func(t *testing.T) {
		rest, images, err := extractImageArgs([]string{"describe", "@" + pngPath, "@handle", "please"})
		if err != nil {
			t.Fatalf("extractImageArgs: %v", err)
		}
		if strings.Join(rest, " ") != "describe @handle please" {
			t.Errorf("rest = %v", rest)
		}
		if len(images) != 1 || images[0].MediaType != "image/png" ||
			images[0].Data != base64.StdEncoding.EncodeToString([]byte("png-bytes")) {
			t.Errorf("images = %#v", images)
		}
	})

	t.Run("missing file errors", func(t *testing.T) {
		if _, _, err := extractImageArgs([]string{"@" + filepath.Join(dir, "nope.png")}); err == nil {
			t.Fatal("expected error for missing image file")
		}
	})

	t.Run("no image args pass through", func(t *testing.T) {
		rest, images, err := extractImageArgs([]string{"just", "a", "query"})
		if err != nil || len(images) != 0 || len(rest) != 3 {
			t.Fatalf("rest=%v images=%v err=%v", rest, images, err)
		}
	})
}
