package main

import (
	"strings"
	"testing"

	"github.com/example/toutiaohao-mcp-server/toutiaohao"
)

func TestEnsureInlineImagesAppendsImagesWhenContentHasNoMarkdownImages(t *testing.T) {
	content := ensureInlineImages("hello", &toutiaohao.ArticleOptions{Images: []string{"/tmp/a.jpg", "/tmp/b.jpg"}})

	if !strings.Contains(content, "![配图1](/tmp/a.jpg)") {
		t.Fatalf("expected first image markdown to be appended, got: %s", content)
	}
	if !strings.Contains(content, "![配图2](/tmp/b.jpg)") {
		t.Fatalf("expected second image markdown to be appended, got: %s", content)
	}
}

func TestEnsureInlineImagesKeepsExistingMarkdownImages(t *testing.T) {
	input := "hello\n\n![已有图](/tmp/existing.jpg)"
	content := ensureInlineImages(input, &toutiaohao.ArticleOptions{Images: []string{"/tmp/a.jpg"}})

	if content != input {
		t.Fatalf("expected existing markdown image content to stay unchanged, got: %s", content)
	}
}
