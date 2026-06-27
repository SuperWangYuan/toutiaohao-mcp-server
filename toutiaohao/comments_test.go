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

func TestCommentItemFromAPI(t *testing.T) {
	apiItem := commentAPIItem{
		IDStr:      "7655725926924206911",
		Text:       "这是一条真实评论",
		CreateTime: 1782488353,
		ReplyCount: 2,
	}
	apiItem.User.Name = "测试用户"
	apiItem.ArticleInfo.GroupIDStr = "7655556701420061184"
	apiItem.ArticleInfo.Title = "测试文章"

	item := commentItemFromAPI(apiItem)
	if item.CommentID != "7655725926924206911" {
		t.Fatalf("CommentID = %q", item.CommentID)
	}
	if item.ArticleID != "7655556701420061184" {
		t.Fatalf("ArticleID = %q", item.ArticleID)
	}
	if item.UserName != "测试用户" {
		t.Fatalf("UserName = %q", item.UserName)
	}
	if item.Content != "这是一条真实评论" {
		t.Fatalf("Content = %q", item.Content)
	}
	if item.ReplyCount != 2 {
		t.Fatalf("ReplyCount = %d", item.ReplyCount)
	}
	if item.CreateTime == "" {
		t.Fatal("CreateTime should be formatted")
	}
	if item.RawText != "测试文章 | 这是一条真实评论" {
		t.Fatalf("RawText = %q", item.RawText)
	}
}

func TestCommentItemFromAPINumericIDFallback(t *testing.T) {
	apiItem := commentAPIItem{ID: 1234567890123, Text: "评论"}
	apiItem.ArticleInfo.GroupID = 7655556701420061184

	item := commentItemFromAPI(apiItem)
	if item.CommentID != "1234567890123" {
		t.Fatalf("CommentID = %q", item.CommentID)
	}
	if item.ArticleID != "7655556701420061184" {
		t.Fatalf("ArticleID = %q", item.ArticleID)
	}
}
