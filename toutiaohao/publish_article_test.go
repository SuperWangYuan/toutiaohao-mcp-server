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
