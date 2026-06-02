package toutiaohao

import "testing"

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
