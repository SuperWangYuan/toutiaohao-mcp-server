package toutiaohao

import (
	"testing"
)

func TestMicroPostValidation_EmptyContent(t *testing.T) {
	err := ValidateMicroPost("", nil, "")
	if err == nil {
		t.Error("expected error for empty content")
	}
	if err.Error() != "content is required" {
		t.Errorf("error = %q, want %q", err.Error(), "content is required")
	}
}

func TestMicroPostValidation_ContentTooLong(t *testing.T) {
	content := make([]rune, 2001)
	for i := range content {
		content[i] = '测'
	}
	err := ValidateMicroPost(string(content), nil, "")
	if err == nil {
		t.Error("expected error for content too long")
	}
}

func TestMicroPostValidation_ContentAtLimit(t *testing.T) {
	content := make([]rune, 2000)
	for i := range content {
		content[i] = '测'
	}
	err := ValidateMicroPost(string(content), nil, "")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMicroPostValidation_TooManyImages(t *testing.T) {
	images := make([]string, 10)
	for i := range images {
		images[i] = "img.jpg"
	}
	err := ValidateMicroPost("hello", images, "")
	if err == nil {
		t.Error("expected error for too many images")
	}
}

func TestMicroPostValidation_NineImages(t *testing.T) {
	images := make([]string, 9)
	for i := range images {
		images[i] = "img.jpg"
	}
	err := ValidateMicroPost("hello", images, "")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSaveMicroDraftRequestBuild(t *testing.T) {
	req := BuildDraftRequest("hello world", nil)
	if req.DraftType != 1 {
		t.Errorf("DraftType = %d, want 1", req.DraftType)
	}
	if req.DraftInfo == "" {
		t.Error("DraftInfo should not be empty")
	}
	if req.PostID != "" {
		t.Errorf("PostID = %q, want empty", req.PostID)
	}
}
