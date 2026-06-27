package main

import (
	"strings"
	"testing"

	"github.com/example/toutiaohao-mcp-server/toutiaohao"
)

func TestArticleStatusIsDraft(t *testing.T) {
	cases := []interface{}{float64(1), int(1), int64(1), "1", "draft", "草稿"}
	for _, tc := range cases {
		if !toutiaohao.ArticleStatusIsDraft(tc) {
			t.Fatalf("expected %v to be treated as draft", tc)
		}
	}
}

func TestArticleStatusIsDraftFalse(t *testing.T) {
	cases := []interface{}{float64(3), int(3), int64(3), "3", "published", nil}
	for _, tc := range cases {
		if toutiaohao.ArticleStatusIsDraft(tc) {
			t.Fatalf("expected %v not to be treated as draft", tc)
		}
	}
}

func TestArticleStatusIsPublished(t *testing.T) {
	cases := []interface{}{float64(3), int(3), int64(3), float64(6), int(6), int64(6), "3", "6", "published", "已发布", "审核中", "已提交"}
	for _, tc := range cases {
		if !toutiaohao.ArticleStatusIsPublished(tc) {
			t.Fatalf("expected %v to be treated as published", tc)
		}
	}
}

func TestArticleStatusIsPublishedFalse(t *testing.T) {
	cases := []interface{}{float64(1), int(1), int64(1), "1", "draft", "草稿", nil}
	for _, tc := range cases {
		if toutiaohao.ArticleStatusIsPublished(tc) {
			t.Fatalf("expected %v not to be treated as published", tc)
		}
	}
}

func TestArticlePublishDedupeBlocksInFlightDuplicate(t *testing.T) {
	resetArticlePublishDedupeForTest()
	t.Cleanup(resetArticlePublishDedupeForTest)

	key := articlePublishDedupeKey("标题", "正文", &toutiaohao.ArticleOptions{
		Images: []string{"cover.png"},
	})
	finish, err := beginArticlePublishDedupe(key, "标题")
	if err != nil {
		t.Fatalf("beginArticlePublishDedupe() unexpected error: %v", err)
	}
	defer finish(false)

	if _, err := beginArticlePublishDedupe(key, "标题"); err == nil || !strings.Contains(err.Error(), "正在发布中") {
		t.Fatalf("duplicate in-flight publish was not blocked, err=%v", err)
	}
}

func TestArticlePublishDedupeBlocksRecentCompletedDuplicate(t *testing.T) {
	resetArticlePublishDedupeForTest()
	t.Cleanup(resetArticlePublishDedupeForTest)

	key := articlePublishDedupeKey("标题", "正文", &toutiaohao.ArticleOptions{
		Images: []string{"cover.png"},
	})
	finish, err := beginArticlePublishDedupe(key, "标题")
	if err != nil {
		t.Fatalf("beginArticlePublishDedupe() unexpected error: %v", err)
	}
	finish(true)

	if _, err := beginArticlePublishDedupe(key, "标题"); err == nil || !strings.Contains(err.Error(), "完成过发布") {
		t.Fatalf("recent completed publish was not blocked, err=%v", err)
	}
}

func TestArticlePublishDedupeKeyIncludesImages(t *testing.T) {
	resetArticlePublishDedupeForTest()
	t.Cleanup(resetArticlePublishDedupeForTest)

	left := articlePublishDedupeKey("标题", "正文", &toutiaohao.ArticleOptions{
		Images: []string{"cover-a.png"},
	})
	right := articlePublishDedupeKey("标题", "正文", &toutiaohao.ArticleOptions{
		Images: []string{"cover-b.png"},
	})
	if left == right {
		t.Fatal("dedupe key should include explicit images")
	}
}
