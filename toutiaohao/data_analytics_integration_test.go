package toutiaohao

import (
	"context"
	"testing"

	"github.com/example/toutiaohao-mcp-server/cookies"
)

// TestGetAccountTrendsManual 验证拉取近 N 天数据趋势的集成测试
func TestGetAccountTrendsManual(t *testing.T) {
	cookieStore := cookies.NewFileCookieStore(cookies.GetDefaultCookiePath())
	cookiesData, err := cookieStore.LoadCookies()
	if err != nil || len(cookiesData) == 0 {
		t.Skip("跳过集成测试：本地没有检测到有效的 cookies.json 凭证文件，请先扫码登录")
		return
	}

	ctx := context.Background()
	// 测试获取近 7 天的数据趋势
	res, err := GetAccountTrends(ctx, 7, cookieStore)
	if err != nil {
		t.Fatalf("获取近 7 天数据趋势失败: %v", err)
	}

	if res == nil {
		t.Fatal("趋势数据响应不能为 nil")
	}

	if res.Days != 7 {
		t.Errorf("返回天数 Days = %d, want 7", res.Days)
	}

	t.Logf("成功拉取近 %d 天趋势数据，条数: %d", res.Days, len(res.Trends))
	for _, trend := range res.Trends {
		t.Logf("日期: %s, 展现: %d, 阅读: %d, 点赞: %d, 评论: %d, 粉丝变化: %d",
			trend.Date, trend.ImpressionCount, trend.ReadCount, trend.LikeCount, trend.CommentCount, trend.FansChangeCount)
	}
}

// TestGetArticleDetailManual 验证拉取单篇文章详情的集成测试
func TestGetArticleDetailManual(t *testing.T) {
	cookieStore := cookies.NewFileCookieStore(cookies.GetDefaultCookiePath())
	cookiesData, err := cookieStore.LoadCookies()
	if err != nil || len(cookiesData) == 0 {
		t.Skip("跳过集成测试：本地没有检测到有效的 cookies.json 凭证文件，请先扫码登录")
		return
	}

	ctx := context.Background()
	// 1. 先通过列表接口拉取最新的一篇文章，获取其 ID，实现无污染的动态测试
	listParams := &ArticleListParams{
		Page:     1,
		PageSize: 5,
		Status:   "all",
	}
	listResp, err := GetArticleList(ctx, listParams, cookieStore)
	if err != nil {
		t.Fatalf("从列表拉取文章 ID 失败: %v", err)
	}

	if listResp == nil || len(listResp.Articles) == 0 {
		t.Skip("跳过测试：当前文章列表为空，无法进行详情拉取测试")
		return
	}

	targetArticle := listResp.Articles[0]
	t.Logf("定位到测试文章: ID=%s, Title=%s", targetArticle.ArticleID, targetArticle.Title)

	// 2. 使用该动态 ID 进行详情测试
	detail, err := GetArticleDetail(ctx, targetArticle.ArticleID, cookieStore)
	if err != nil {
		t.Fatalf("拉取文章详情失败: %v", err)
	}

	if detail == nil {
		t.Fatal("详情数据不能为 nil")
	}

	// 检查返回中是否包含了基本的一些属性（如 title）
	if title, ok := detail["title"].(string); ok {
		t.Logf("详情拉取验证成功！文章标题为: %s", title)
	} else if title, ok := detail["article_title"].(string); ok {
		t.Logf("详情拉取验证成功！文章标题为: %s", title)
	} else {
		t.Log("详情中未找到 title 字段，但已成功返回 Data 字段的键列表: ")
		for k := range detail {
			t.Logf("  Key: %s", k)
		}
	}
}

// TestGetMicroPostsManual 验证拉取微头条列表的集成测试
func TestGetMicroPostsManual(t *testing.T) {
	cookieStore := cookies.NewFileCookieStore(cookies.GetDefaultCookiePath())
	cookiesData, err := cookieStore.LoadCookies()
	if err != nil || len(cookiesData) == 0 {
		t.Skip("跳过集成测试：本地没有检测到有效的 cookies.json 凭证文件，请先扫码登录")
		return
	}

	ctx := context.Background()
	params := &ArticleListParams{
		Page:        1,
		PageSize:    10,
		Status:      "all",
		ContentType: "ugc",
	}

	res, err := GetArticleList(ctx, params, cookieStore)
	if err != nil {
		t.Fatalf("拉取微头条列表失败: %v", err)
	}

	if res == nil {
		t.Fatal("微头条列表响应不能为 nil")
	}

	t.Logf("成功拉取微头条列表，总数 Total: %d, 当前页条数: %d", res.Total, len(res.Articles))
	if len(res.Articles) > 0 {
		for i, art := range res.Articles {
			t.Logf("[%d] ID=%s, Title=%s, 展现: %d, 阅读/播放: %d, 点赞: %d, 评论: %d, CTR: %.4f, URL: %s",
				i, art.ArticleID, art.Title, art.ImpressionCount, art.ReadCount, art.LikeCount, art.CommentCount, art.CTR, art.ArticleURL)
		}
	} else {
		t.Error("预期微头条数量大 0，但实际拉取到 0 条")
	}
}
