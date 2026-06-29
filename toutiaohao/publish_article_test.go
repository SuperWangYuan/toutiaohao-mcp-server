package toutiaohao

import (
	"strings"
	"testing"
)

func TestArticleValidation_EmptyTitle(t *testing.T) {
	err := ValidateArticle("", "content", nil)
	if err == nil {
		t.Error("expected error for empty title")
	}
	if err.Error() != "title is required" {
		t.Errorf("error = %q, want %q", err.Error(), "title is required")
	}
}

func TestArticleValidation_TitleTooLong(t *testing.T) {
	title := make([]rune, 31)
	for i := range title {
		title[i] = '测'
	}
	err := ValidateArticle(string(title), "content", nil)
	if err == nil {
		t.Error("expected error for title too long")
	}
	if !strings.Contains(err.Error(), "系统不会自动截断") {
		t.Fatalf("error should require caller regeneration instead of truncation: %v", err)
	}
}

func TestArticleValidation_TitleAtLimit(t *testing.T) {
	title := make([]rune, 30)
	for i := range title {
		title[i] = '测'
	}
	err := ValidateArticle(string(title), "content", nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestArticleValidation_TitleCountsUnicodeCharacters(t *testing.T) {
	title := "国行iPhone用户等待AI功能"
	if got := len([]rune(title)); got > 30 {
		t.Fatalf("test title unexpectedly has %d characters", got)
	}
	if err := ValidateArticle(title, "content", nil); err != nil {
		t.Fatalf("ValidateArticle() rejected a valid mixed Chinese/ASCII title: %v", err)
	}
}

func TestArticleValidation_EmptyContent(t *testing.T) {
	err := ValidateArticle("title", "", nil)
	if err == nil {
		t.Error("expected error for empty content")
	}
	if err.Error() != "content is required" {
		t.Errorf("error = %q, want %q", err.Error(), "content is required")
	}
}

func TestArticleValidation_ContentTooLong(t *testing.T) {
	content := make([]rune, 50001)
	for i := range content {
		content[i] = 'a'
	}
	err := ValidateArticle("title", string(content), nil)
	if err == nil {
		t.Error("expected error for content too long")
	}
}

func TestArticleValidation_ValidMinimal(t *testing.T) {
	err := ValidateArticle("title", "content", nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestArticleValidation_WithAllOptional(t *testing.T) {
	err := ValidateArticle("title", "content", &ArticleOptions{
		Images:     []string{"img.jpg"},
		Tags:       []string{"tag1"},
		Category:   "tech",
		CoverImage: "cover.jpg",
		Original:   true,
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestShouldBypassHijackForImageUpload(t *testing.T) {
	cases := []struct {
		name        string
		url         string
		method      string
		contentType string
		want        bool
	}{
		{
			name:   "spice image upload",
			url:    "https://mp.toutiao.com/spice/image?upload_source=20020002&aid=1231&device_platform=web",
			method: "POST",
			want:   true,
		},
		{
			name:        "multipart upload",
			url:         "https://mp.toutiao.com/mp/agw/article/submit",
			method:      "POST",
			contentType: "multipart/form-data; boundary=abc",
			want:        true,
		},
		{
			name:   "star api stays hijacked",
			url:    "https://mp.toutiao.com/mp/agw/media/get_user_base_info",
			method: "GET",
			want:   false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldBypassHijack(tt.url, tt.method, tt.contentType); got != tt.want {
				t.Fatalf("shouldBypassHijack() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDecideArticleCoverPrioritizesExplicitImages(t *testing.T) {
	inlineImages := []string{"inline-1.png", "inline-2.png", "inline-3.png"}
	decision := decideArticleCover(&ArticleOptions{
		Images: []string{"cover.png"},
	}, inlineImages)

	if decision.Mode != "单图" {
		t.Fatalf("mode = %q, want 单图", decision.Mode)
	}
	if decision.Auto {
		t.Fatal("explicit images should not be treated as auto cover")
	}
	if len(decision.Covers) != 1 || decision.Covers[0] != "cover.png" {
		t.Fatalf("covers = %#v, want explicit cover image", decision.Covers)
	}
}

func TestDecideArticleCoverFallsBackToInlineImages(t *testing.T) {
	decision := decideArticleCover(nil, []string{"inline-1.png", "inline-2.png", "inline-3.png"})

	if decision.Mode != "三图" {
		t.Fatalf("mode = %q, want 三图", decision.Mode)
	}
	if !decision.Auto {
		t.Fatal("inline images fallback should be marked auto")
	}
	if len(decision.Covers) != 3 || decision.Covers[0] != "inline-1.png" {
		t.Fatalf("covers = %#v, want first three inline images", decision.Covers)
	}
}

func TestDecideArticleCoverPrioritizesCoverImage(t *testing.T) {
	decision := decideArticleCover(&ArticleOptions{
		CoverImage: "cover-image.png",
		Images:     []string{"image-cover.png", "image-cover-2.png", "image-cover-3.png"},
	}, []string{"inline-1.png", "inline-2.png", "inline-3.png"})

	if decision.Mode != "单图" {
		t.Fatalf("mode = %q, want 单图", decision.Mode)
	}
	if len(decision.Covers) != 1 || decision.Covers[0] != "cover-image.png" {
		t.Fatalf("covers = %#v, want cover_image priority", decision.Covers)
	}
}

func TestHasPublishTime(t *testing.T) {
	cases := []struct {
		name  string
		value interface{}
		want  bool
	}{
		{name: "nil", value: nil, want: false},
		{name: "empty string", value: "", want: false},
		{name: "blank string", value: "  ", want: false},
		{name: "formatted string", value: "2026-06-10 18:00", want: true},
		{name: "unix timestamp", value: int64(1781085600), want: true},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasPublishTime(tt.value); got != tt.want {
				t.Fatalf("hasPublishTime(%#v) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}
