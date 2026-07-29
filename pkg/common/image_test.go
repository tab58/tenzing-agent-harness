package common

import "testing"

func TestNewImageContent(t *testing.T) {
	block := NewImageContent("image/png", "aGVsbG8=")
	if block.Type != ContentTypeImage {
		t.Errorf("type = %q, want %q", block.Type, ContentTypeImage)
	}
	if block.Image == nil {
		t.Fatal("Image not populated")
	}
	if block.Image.MediaType != "image/png" || block.Image.Data != "aGVsbG8=" {
		t.Errorf("image = %+v, want image/png / aGVsbG8=", *block.Image)
	}
}

func TestCombinedTextExcludesImages(t *testing.T) {
	blocks := []ContentBlock{
		NewTextContent("what is "),
		NewImageContent("image/png", "aGVsbG8="),
		NewTextContent("this?"),
	}
	if got := CombinedText(blocks); got != "what is this?" {
		t.Errorf("CombinedText = %q, want image data excluded", got)
	}
}
