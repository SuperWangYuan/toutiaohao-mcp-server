package main

import "testing"

func TestArticleStatusIsDraft(t *testing.T) {
	cases := []interface{}{float64(1), int(1), int64(1), "1", "draft", "草稿"}
	for _, tc := range cases {
		if !articleStatusIsDraft(tc) {
			t.Fatalf("expected %v to be treated as draft", tc)
		}
	}
}

func TestArticleStatusIsDraftFalse(t *testing.T) {
	cases := []interface{}{float64(3), int(3), int64(3), "3", "published", nil}
	for _, tc := range cases {
		if articleStatusIsDraft(tc) {
			t.Fatalf("expected %v not to be treated as draft", tc)
		}
	}
}
