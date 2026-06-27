package toutiaohao

import "testing"

func TestReplyCommentValidation_EmptyReplyContent(t *testing.T) {
	err := ValidateReplyComment("123", "456", "", "")
	if err == nil {
		t.Fatal("expected error for empty reply_content")
	}
}

func TestReplyCommentValidation_MissingLocator(t *testing.T) {
	err := ValidateReplyComment("123", "", "", "谢谢支持")
	if err == nil {
		t.Fatal("expected error when comment_id and comment_text are both empty")
	}
}

func TestReplyCommentValidation_ValidWithCommentID(t *testing.T) {
	if err := ValidateReplyComment("123", "456", "", "谢谢支持"); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestReplyCommentValidation_ValidWithCommentText(t *testing.T) {
	if err := ValidateReplyComment("", "", "写得不错", "谢谢支持"); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestNewCommentListParams(t *testing.T) {
	params := NewCommentListParams(map[string]interface{}{
		"article_id": " 123 ",
		"keyword":    " 关键 ",
		"page_size":  float64(12),
	})
	if params.ArticleID != "123" {
		t.Fatalf("ArticleID = %q, want 123", params.ArticleID)
	}
	if params.Keyword != "关键" {
		t.Fatalf("Keyword = %q, want 关键", params.Keyword)
	}
	if params.PageSize != 12 {
		t.Fatalf("PageSize = %d, want 12", params.PageSize)
	}
}
