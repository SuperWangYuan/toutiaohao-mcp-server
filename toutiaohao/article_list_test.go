package toutiaohao

import (
	"testing"
)

func TestArticleListDefaultParams(t *testing.T) {
	params := NewArticleListParams(nil)
	if params.Page != 1 {
		t.Errorf("Page = %d, want 1", params.Page)
	}
	if params.PageSize != 20 {
		t.Errorf("PageSize = %d, want 20", params.PageSize)
	}
	if params.Status != "all" {
		t.Errorf("Status = %s, want all", params.Status)
	}
}

func TestArticleListValidStatus(t *testing.T) {
	for _, status := range []string{"all", "published", "draft", "review"} {
		err := ValidateArticleListStatus(status)
		if err != nil {
			t.Errorf("ValidateArticleListStatus(%q) = %v, want nil", status, err)
		}
	}
}

func TestArticleListInvalidStatus(t *testing.T) {
	err := ValidateArticleListStatus("unknown")
	if err == nil {
		t.Error("expected error for invalid status")
	}
}

func TestArticleListPageValidation(t *testing.T) {
	params := NewArticleListParams(map[string]interface{}{
		"page":      0,
		"page_size": 0,
	})
	if params.Page != 1 {
		t.Errorf("Page = %d, want 1 (auto-corrected)", params.Page)
	}
	if params.PageSize != 20 {
		t.Errorf("PageSize = %d, want 20 (auto-corrected)", params.PageSize)
	}
}

func TestDeleteArticleValidation_EmptyID(t *testing.T) {
	err := ValidateDeleteArticle("")
	if err == nil {
		t.Error("expected error for empty article_id")
	}
	if err.Error() != "article_id is required" {
		t.Errorf("error = %q, want %q", err.Error(), "article_id is required")
	}
}

func TestDeleteArticleValidation_ValidID(t *testing.T) {
	err := ValidateDeleteArticle("12345")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
