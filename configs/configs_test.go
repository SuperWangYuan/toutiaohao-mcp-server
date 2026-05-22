package configs

import "testing"

func TestConfigURLsNotEmpty(t *testing.T) {
	urls := map[string]string{
		"LoginPage":      LoginPage,
		"Homepage":       Homepage,
		"PublishArticle": PublishArticle,
		"PublishMicro":   PublishMicro,
		"ArticleListAPI": ArticleListAPI,
		"DeleteArticleAPI": DeleteArticleAPI,
		"UploadImageAPI":   UploadImageAPI,
	}

	for name, url := range urls {
		if url == "" {
			t.Errorf("URL constant %s is empty", name)
		}
	}
}

func TestContentLimits(t *testing.T) {
	if MaxTitleLength != 100 {
		t.Errorf("MaxTitleLength = %d, want 100", MaxTitleLength)
	}
	if MaxWeitoutiaoLength != 2000 {
		t.Errorf("MaxWeitoutiaoLength = %d, want 2000", MaxWeitoutiaoLength)
	}
	if MaxImagesPerPost != 9 {
		t.Errorf("MaxImagesPerPost = %d, want 9", MaxImagesPerPost)
	}
	if MaxContentLength != 50000 {
		t.Errorf("MaxContentLength = %d, want 50000", MaxContentLength)
	}
	if MaxImageSize != 10*1024*1024 {
		t.Errorf("MaxImageSize = %d, want %d", MaxImageSize, 10*1024*1024)
	}
}
