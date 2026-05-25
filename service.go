package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/example/toutiaohao-mcp-server/browser"
	"github.com/example/toutiaohao-mcp-server/configs"
	"github.com/example/toutiaohao-mcp-server/cookies"
	"github.com/example/toutiaohao-mcp-server/toutiaohao"
	log "github.com/sirupsen/logrus"
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
func (s *ToutiaoService) PublishMicroPost(ctx context.Context, content string, images []string, topic string, publishTime interface{}) error {
	if err := toutiaohao.ValidateMicroPost(content, images, topic); err != nil {
		return err
	}

	b := browser.NewBrowser(false)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := toutiaohao.NewMicroPostAction(page, s.cookieStore)
	return action.Publish(ctx, content, images, topic, publishTime)
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

// UpdateArticle 修改/更新文章
func (s *ToutiaoService) UpdateArticle(ctx context.Context, articleID string, title, content string, opts *toutiaohao.ArticleOptions) error {
	if err := toutiaohao.ValidateUpdateArticle(articleID, title, content, opts); err != nil {
		return err
	}

	b := browser.NewBrowser(false)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := toutiaohao.NewArticlePublishAction(page, s.cookieStore)
	return action.Update(ctx, articleID, title, content, opts)
}

// QrCodeLogin 独立的交互式扫码登录方法，专为在不受MCP超时限制的CLI环境下进行登录捕获
func (s *ToutiaoService) QrCodeLogin(ctx context.Context) error {
	// 启动非无头浏览器
	b := browser.NewBrowser(false)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	// 导航到头条登录页
	log.Info("正在导航到今日头条登录页面，请准备在弹出的 Chrome 窗口中扫码...")
	if err := page.Navigate(configs.LoginPage); err != nil {
		return fmt.Errorf("导航到登录页失败: %w", err)
	}
	_ = page.WaitLoad()

	// 轮询等待登录成功（最大5分钟）
	timeout := 300 * time.Second
	interval := 1 * time.Second
	deadline := time.Now().Add(timeout)

	log.Warn("=================================================================")
	log.Warn("【安全提示】交互式扫码登录已启动！")
	log.Warn("请在弹出的 Chrome 浏览器窗口中，及时使用手机微信/今日头条App扫码登录...")
	log.Warn("登录成功后，程序会自动保存Cookie凭证并自动关闭浏览器。")
	log.Warn("=================================================================")

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		currentInfo, err := page.Info()
		if err == nil {
			if toutiaohao.IsLoginSuccessURL(currentInfo.URL) || 
			   (!strings.Contains(currentInfo.URL, "login") && !strings.Contains(currentInfo.URL, "auth") && strings.Contains(currentInfo.URL, "mp.toutiao.com")) {
				log.Info("检测到扫码登录成功！")
				
				// 延迟一秒等待 Cookie 完全写入浏览器内存
				time.Sleep(1 * time.Second)
				
				// 自动回写 Cookie 到本地
				if err := toutiaohao.SaveBrowserCookies(page, s.cookieStore); err != nil {
					return fmt.Errorf("自动保存新 Cookie 失败: %w", err)
				}
				log.Info("新 Cookie 已成功保存！登录凭证已持久化写入 cookies.json。")
				return nil
			}
		}
		time.Sleep(interval)
	}

	return fmt.Errorf("扫码登录超时（已等待 5 分钟），请重新运行并及时扫码")
}
