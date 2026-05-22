package main

import (
	"context"

	"github.com/example/toutiaohao-mcp-server/browser"
	"github.com/example/toutiaohao-mcp-server/cookies"
	"github.com/example/toutiaohao-mcp-server/toutiaohao"
)

// ToutiaoService 今日头条业务服务层
type ToutiaoService struct {
	cookieStore cookies.Cookier
}

// NewToutiaoService 创建服务实例
func NewToutiaoService(cookieStore cookies.Cookier) *ToutiaoService {
	return &ToutiaoService{
		cookieStore: cookieStore,
	}
}

// LoginWithCredentials 账密登录
func (s *ToutiaoService) LoginWithCredentials(ctx context.Context, username, password string) (*toutiaohao.LoginResponse, error) {
	b := browser.NewBrowser(false)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := toutiaohao.NewLoginAction(page, s.cookieStore)
	return action.Login(ctx, username, password)
}

// CheckLoginStatus 检查登录状态
func (s *ToutiaoService) CheckLoginStatus(ctx context.Context) (*toutiaohao.LoginStatusResponse, error) {
	return toutiaohao.CheckLoginStatus(s.cookieStore)
}

// DeleteCookies 删除 Cookie
func (s *ToutiaoService) DeleteCookies(ctx context.Context) error {
	return s.cookieStore.DeleteCookies()
}

// PublishMicroPost 发布微头条
func (s *ToutiaoService) PublishMicroPost(ctx context.Context, content string, images []string, topic string) error {
	if err := toutiaohao.ValidateMicroPost(content, images, topic); err != nil {
		return err
	}

	b := browser.NewBrowser(false)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := toutiaohao.NewMicroPostAction(page, s.cookieStore)
	return action.Publish(ctx, content, images, topic)
}

// SaveMicroPostDraft 保存微头条草稿
func (s *ToutiaoService) SaveMicroPostDraft(ctx context.Context, content string, images []string, topic string) error {
	if err := toutiaohao.ValidateMicroPost(content, images, topic); err != nil {
		return err
	}
	return toutiaohao.SaveMicroDraft(ctx, content, nil, s.cookieStore)
}

// PublishArticle 发布文章
func (s *ToutiaoService) PublishArticle(ctx context.Context, title, content string, opts *toutiaohao.ArticleOptions) error {
	if err := toutiaohao.ValidateArticle(title, content, opts); err != nil {
		return err
	}

	b := browser.NewBrowser(false)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := toutiaohao.NewArticlePublishAction(page, s.cookieStore)
	return action.Publish(ctx, title, content, opts)
}

// GetArticleList 获取文章列表
func (s *ToutiaoService) GetArticleList(ctx context.Context, params *toutiaohao.ArticleListParams) (*toutiaohao.ArticleListResponse, error) {
	return toutiaohao.GetArticleList(ctx, params, s.cookieStore)
}

// DeleteArticle 删除文章
func (s *ToutiaoService) DeleteArticle(ctx context.Context, articleID string) error {
	return toutiaohao.DeleteArticle(ctx, articleID, s.cookieStore)
}

// GetAccountOverview 获取账户概览（通过浏览器自动化）
func (s *ToutiaoService) GetAccountOverview(ctx context.Context) (*toutiaohao.AccountOverview, error) {
	b := browser.NewBrowser(false)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	return toutiaohao.GetAccountOverview(ctx, page, s.cookieStore)
}

// GetArticleStats 获取文章统计
func (s *ToutiaoService) GetArticleStats(ctx context.Context, articleID string) (*toutiaohao.ArticleStats, error) {
	return toutiaohao.GetArticleStats(ctx, articleID, s.cookieStore)
}

// GenerateReport 生成分析报告（通过浏览器自动化）
func (s *ToutiaoService) GenerateReport(ctx context.Context, reportType string) (*toutiaohao.Report, error) {
	b := browser.NewBrowser(false)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	return toutiaohao.GenerateReport(ctx, reportType, page, s.cookieStore)
}
