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

// CheckArticleExists 检查最新文章中是否已存在该标题的文章
func (s *ToutiaoService) CheckArticleExists(ctx context.Context, title string) (bool, error) {
	params := &toutiaohao.ArticleListParams{
		Page:     1,
		PageSize: 10,
		Status:   "all",
	}
	resp, err := s.GetArticleList(ctx, params)
	if err != nil {
		return false, fmt.Errorf("check article exists failed to get article list: %w", err)
	}
	for _, art := range resp.Articles {
		if art.Title == title {
			return true, nil
		}
	}
	return false, nil
}

// PublishArticle 发布文章
func (s *ToutiaoService) PublishArticle(ctx context.Context, title, content string, opts *toutiaohao.ArticleOptions) (*toutiaohao.PublishResult, error) {
	log.Infof("[Step 1/7] 开始发布文章校验，标题: %s", title)
	title = truncateTitleForPublish(title)
	if err := toutiaohao.ValidateArticle(title, content, opts); err != nil {
		log.Errorf("[Step 1/7] 参数校验失败: %v", err)
		return nil, err
	}

	// 0. 发文前登录态快速校验（通过轻量级 HTTP 请求校验本地 Cookie，耗时 ~100ms）
	log.Info("[Step 2/7] 正在快速自检本地 Cookie 登录态...")
	status, errAuth := s.CheckLoginStatus(ctx)
	if errAuth != nil || status == nil || !status.LoggedIn {
		errVal := fmt.Errorf("发文前登录态校验失败，本地 cookies.json 已过期或失效，请先运行扫码登录（运行命令: ./toutiaohao-server -login）")
		log.Errorf("[Step 2/7] 登录态校验未通过: %v", errVal)
		return nil, errVal
	}
	log.Info("[Step 2/7] 登录态自检通过！")

	// 1. 发布前检查：检查标题是否已存在，避免重复发布
	log.Info("[Step 3/7] 正在发起查重，核对最新文章列表中是否已存在同名文章...")
	exists, err := s.CheckArticleExists(ctx, title)
	if err == nil && exists {
		errExists := fmt.Errorf("标题「%s」已存在，请勿重复发布", title)
		log.Errorf("[Step 3/7] 查重拦截: %v", errExists)
		return nil, errExists
	} else if err != nil {
		log.Warnf("[Step 3/7] 检查文章标题是否存在时发生错误: %v，将继续尝试发布...", err)
	} else {
		log.Info("[Step 3/7] 查重检索完毕，未发现同名冲突。")
	}

	// images 优化警告
	if !strings.Contains(content, "![") || !strings.Contains(content, "](") {
		if opts != nil && len(opts.Images) == 0 && opts.CoverImage == "" {
			log.Warn("[Step 4/7] 未提供任何封面图片，且正文中无插图，系统将自动进入无封面模式")
		}
	}

	log.Info("[Step 5/7] 启动浏览器发文实体操作...")
	b := browser.NewBrowser(false)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := toutiaohao.NewArticlePublishAction(page, s.cookieStore)
	err = action.Publish(ctx, title, content, opts)
	if err != nil {
		log.Errorf("[Step 5/7] 物理执行文章内容键入与发布失败: %v", err)
		return nil, err
	}

	log.Info("[Step 6/7] 物理发布指令提交完毕，等待同步进行发布后验证...")
	log.Info("文章发布完成，等待3秒后获取最新列表进行核对...")
	time.Sleep(3 * time.Second) // 等待后台同步

	coverStatus := "无封面"
	if opts != nil {
		if len(opts.Images) >= 3 {
			coverStatus = "三图"
		} else if opts.CoverImage != "" || len(opts.Images) > 0 {
			coverStatus = "单图"
		} else if strings.Contains(content, "![") && strings.Contains(content, "](") {
			coverStatus = "自适应封面"
		} else {
			coverStatus = "无封面 (未提供任何封面图片)"
		}
	} else {
		coverStatus = "无封面 (未提供任何封面图片)"
	}

	originalStatus := "非原创"
	if opts != nil && opts.Original {
		originalStatus = "原创"
	}

	respList, errList := s.GetArticleList(ctx, &toutiaohao.ArticleListParams{Page: 1, PageSize: 5, Status: "all"})
	if errList == nil && respList != nil && len(respList.Articles) > 0 {
		for _, art := range respList.Articles {
			if art.Title == title {
				if articleStatusIsDraft(art.Status) {
					errDraft := fmt.Errorf("文章提交后仍为草稿状态，ArticleID=%s status=%v", art.ArticleID, art.Status)
					log.Errorf("发布后核对失败：%v", errDraft)
					return nil, errDraft
				}
				log.Infof("发布后核对验证成功：列表中已找到标题为「%s」的文章，ArticleID 为 %s", title, art.ArticleID)
				return &toutiaohao.PublishResult{
					Success:        true,
					Message:        "文章发布成功并通过列表验证",
					ArticleID:      art.ArticleID,
					CoverStatus:    coverStatus,
					OriginalStatus: originalStatus,
				}, nil
			}
		}
	}

	log.Warn("在最新列表中未匹配到刚才发布的文章，可能存在网络或系统同步延迟")
	return &toutiaohao.PublishResult{
		Success:        true,
		Message:        "文章发布完成，但暂未在列表中检测到，可能存在延迟",
		CoverStatus:    coverStatus,
		OriginalStatus: originalStatus,
	}, nil
}

func articleStatusIsDraft(status interface{}) bool {
	switch v := status.(type) {
	case float64:
		return int(v) == 1
	case int:
		return v == 1
	case int64:
		return v == 1
	case string:
		normalized := strings.TrimSpace(strings.ToLower(v))
		return normalized == "1" || normalized == "draft" || normalized == "草稿"
	default:
		return false
	}
}

// GetArticleList 获取文章列表
func (s *ToutiaoService) GetArticleList(ctx context.Context, params *toutiaohao.ArticleListParams) (*toutiaohao.ArticleListResponse, error) {
	return toutiaohao.GetArticleList(ctx, params, s.cookieStore)
}

// DeleteArticle 删除文章
func (s *ToutiaoService) DeleteArticle(ctx context.Context, articleID string) error {
	// 先用 HTTP API 尝试删除（适用于已发布/审核中的文章）
	err := toutiaohao.DeleteArticle(ctx, articleID, s.cookieStore)
	if err == nil {
		return nil
	}

	// 如果 HTTP 删除失败（可能是草稿/待审核状态），回退到浏览器自动化
	log.Warnf("HTTP API 删除失败: %v，回退到浏览器自动化删除...", err)
	articleTitle := s.findArticleTitleForDelete(ctx, articleID)

	b := browser.NewBrowser(false)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	return toutiaohao.DeleteDraftByBrowserWithTitle(ctx, page, articleID, articleTitle)
}

func (s *ToutiaoService) findArticleTitleForDelete(ctx context.Context, articleID string) string {
	statuses := []string{"draft", "all"}
	for _, status := range statuses {
		resp, err := s.GetArticleList(ctx, &toutiaohao.ArticleListParams{Page: 1, PageSize: 20, Status: status})
		if err != nil || resp == nil {
			continue
		}
		for _, article := range resp.Articles {
			if article.ArticleID == articleID || article.ID == articleID || strings.Contains(article.ArticleURL, articleID) {
				log.Infof("删除回退定位到文章标题: %s", article.Title)
				return article.Title
			}
		}
	}
	log.Warnf("删除回退未能从列表定位文章标题，仅使用 ID 删除: %s", articleID)
	return ""
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
func (s *ToutiaoService) UpdateArticle(ctx context.Context, articleID string, title, content string, opts *toutiaohao.ArticleOptions) (*toutiaohao.PublishResult, error) {
	if err := toutiaohao.ValidateUpdateArticle(articleID, title, content, opts); err != nil {
		return nil, err
	}

	b := browser.NewBrowser(false)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := toutiaohao.NewArticlePublishAction(page, s.cookieStore)
	err := action.Update(ctx, articleID, title, content, opts)
	if err != nil {
		return nil, err
	}

	// 更新后验证：获取最新文章列表并核对
	log.Info("文章更新完成，等待3秒后获取最新列表进行核对...")
	time.Sleep(3 * time.Second) // 等待后台同步

	coverStatus := "保留原封面"
	if opts != nil && (opts.CoverImage != "" || len(opts.Images) > 0) {
		if len(opts.Images) >= 3 {
			coverStatus = "三图"
		} else {
			coverStatus = "单图"
		}
	}

	originalStatus := "非原创"
	if opts != nil && opts.Original {
		originalStatus = "原创"
	}

	respList, errList := s.GetArticleList(ctx, &toutiaohao.ArticleListParams{Page: 1, PageSize: 5, Status: "all"})
	if errList == nil && respList != nil && len(respList.Articles) > 0 {
		for _, art := range respList.Articles {
			if art.ArticleID == articleID {
				log.Infof("修改后核对验证成功：列表中已找到ID为 %s 的文章", articleID)
				return &toutiaohao.PublishResult{
					Success:        true,
					Message:        "文章更新成功并通过列表验证",
					ArticleID:      art.ArticleID,
					CoverStatus:    coverStatus,
					OriginalStatus: originalStatus,
				}, nil
			}
		}
	}

	log.Warn("在最新列表中未核对到刚才更新的文章，可能存在系统延迟")
	return &toutiaohao.PublishResult{
		Success:        true,
		Message:        "文章更新完成，但暂未在列表中确认到，可能存在延迟",
		ArticleID:      articleID,
		CoverStatus:    coverStatus,
		OriginalStatus: originalStatus,
	}, nil
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
