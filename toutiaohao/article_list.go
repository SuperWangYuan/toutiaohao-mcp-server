package toutiaohao

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/example/toutiaohao-mcp-server/configs"
	"github.com/example/toutiaohao-mcp-server/cookies"
	"github.com/go-rod/rod"
	log "github.com/sirupsen/logrus"
)

var validStatuses = map[string]bool{
	"all": true, "published": true, "draft": true, "review": true,
}

// ArticleListParams 文章列表查询参数
type ArticleListParams struct {
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
	Status   string `json:"status"`
}

// ArticleListResponse 文章列表响应
type ArticleListResponse struct {
	Articles []ArticleItem `json:"articles"`
	Total    int           `json:"total"`
}

// ArticleItem 文章条目
type ArticleItem struct {
	ArticleID     string      `json:"article_id"`
	ID            string      `json:"id"`
	Title         string      `json:"title"`
	Status        interface{} `json:"status"`
	CreateTime    interface{} `json:"create_time"`
	ReadCount     int         `json:"go_detail_count_v2"`
	CommentCount  int         `json:"comment_count"`
	DiggCount     int         `json:"digg_count"`
	ImpressionCount int       `json:"impression_count"`
	ArticleURL    string      `json:"article_url"`
}

// NewArticleListParams 创建文章列表参数（含默认值）
func NewArticleListParams(args map[string]interface{}) *ArticleListParams {
	params := &ArticleListParams{
		Page:     1,
		PageSize: 20,
		Status:   "all",
	}
	if args == nil {
		return params
	}

	if p, ok := args["page"].(int); ok && p > 0 {
		params.Page = p
	} else if pf, ok := args["page"].(float64); ok && pf > 0 {
		params.Page = int(pf)
	}
	if ps, ok := args["page_size"].(int); ok && ps > 0 {
		params.PageSize = ps
	} else if psf, ok := args["page_size"].(float64); ok && psf > 0 {
		params.PageSize = int(psf)
	}
	if s, ok := args["status"].(string); ok && s != "" {
		params.Status = s
	}

	return params
}

// ValidateArticleListStatus 校验状态参数
func ValidateArticleListStatus(status string) error {
	if !validStatuses[status] {
		return fmt.Errorf("invalid status '%s', valid values: all, published, draft, review", status)
	}
	return nil
}

// ValidateDeleteArticle 校验删除文章参数
func ValidateDeleteArticle(articleID string) error {
	if strings.TrimSpace(articleID) == "" {
		return fmt.Errorf("article_id is required")
	}
	return nil
}

// GetArticleList 获取文章列表
func GetArticleList(ctx context.Context, params *ArticleListParams, cookieStore cookies.Cookier) (*ArticleListResponse, error) {
	if err := ValidateArticleListStatus(params.Status); err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s?page=%d&page_size=%d&status=%s",
		configs.ArticleListAPI, params.Page, params.PageSize, params.Status)

	body, err := doAuthenticatedGet(ctx, url, cookieStore)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data struct {
			Articles []ArticleItem `json:"articles"`
			Total    int           `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// 设置 article_id（优先用 id 字段，回退到 pgc_id）
	for i := range resp.Data.Articles {
		if resp.Data.Articles[i].ID != "" {
			resp.Data.Articles[i].ArticleID = resp.Data.Articles[i].ID
		}
	}

	return &ArticleListResponse{
		Articles: resp.Data.Articles,
		Total:    resp.Data.Total,
	}, nil
}

// DeleteArticle 删除文章
func DeleteArticle(ctx context.Context, articleID string, cookieStore cookies.Cookier) error {
	if err := ValidateDeleteArticle(articleID); err != nil {
		return err
	}

	url := configs.DeleteArticleAPI
	body := fmt.Sprintf(`{"article_id":"%s"}`, articleID)

	data, err := cookieStore.LoadCookies()
	if err != nil || data == nil {
		return fmt.Errorf("no cookies available, please login first")
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	injectCookies(req, data)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("delete request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("delete failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	// 头条API可能返回HTTP 200但code非0（如user not login）
	var apiResp struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(respBody, &apiResp); err == nil && apiResp.Code != 0 {
		return fmt.Errorf("delete API returned error code %d: %s", apiResp.Code, string(respBody))
	}

	return nil
}

// doAuthenticatedGet 带 Cookie 的 GET 请求
func doAuthenticatedGet(ctx context.Context, url string, cookieStore cookies.Cookier) ([]byte, error) {
	data, err := cookieStore.LoadCookies()
	if err != nil || data == nil {
		return nil, fmt.Errorf("no cookies available, please login first")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	injectCookies(req, data)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	log.Infof("API [GET %s] status=%d content-type=%s body_len=%d", url, resp.StatusCode, resp.Header.Get("Content-Type"), len(body))
	if len(body) > 500 {
		log.Infof("API response (first 500 bytes): %s", string(body[:500]))
	} else {
		log.Infof("API response: %s", string(body))
	}
	return body, nil
}

// injectCookies 注入 Cookie 到 HTTP 请求
func injectCookies(req *http.Request, cookieData []byte) {
	var cookieList []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(cookieData, &cookieList); err != nil {
		return
	}
	for _, c := range cookieList {
		req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	}
}

// DeleteDraftByBrowser 用浏览器删除草稿（HTTP API 不支持删除草稿状态的文章）
func DeleteDraftByBrowser(ctx context.Context, page *rod.Page, articleID string) error {
	log.Infof("正在用浏览器删除草稿: %s", articleID)

	// 导航到头条号主页，确保 cookie 生效
	if err := page.Navigate("https://mp.toutiao.com"); err != nil {
		return fmt.Errorf("导航到头条主页失败: %w", err)
	}
	_ = page.Timeout(10 * time.Second).WaitLoad()
	time.Sleep(3 * time.Second)

	// 导航到草稿列表页
	if err := page.Navigate("https://mp.toutiao.com/profile_v4/graphic/list?status=1"); err != nil {
		return fmt.Errorf("导航到草稿列表页失败: %w", err)
	}
	_ = page.Timeout(10 * time.Second).WaitLoad()
	time.Sleep(4 * time.Second)


	// 输出页面标题和 URL 以确认已登录
	url, _ := page.Eval(`() => window.location.href`)
	title, _ := page.Eval(`() => document.title`)
	log.Infof("当前页面URL: %v, 标题: %v", url, title)

	// 查找文章列表中的"更多"操作菜单和"删除"按钮
	// 头条草稿列表中每个文章卡片通常有一个"更多"按钮或直接有删除/编辑按钮
	log.Info("尝试定位并点击删除按钮...")

	// JS: 通过文章标题找到对应的卡片，再找卡片内的删除按钮
	// 测试文章的标题含"测试"
	result, err := page.Eval(`(articleID) => {
		// 获取所有可见的卡片元素
		let cards = document.querySelectorAll('div[class*="item"], div[class*="card"], li[class*="item"], tr');
		for (let card of cards) {
			let html = card.innerHTML || '';
			let text = (card.textContent || '').trim();
			if (html.includes(articleID) || text.includes(articleID)) {
				// 找到了！在该卡片范围内找删除按钮
				let btns = card.querySelectorAll('button, span, a');
				for (let btn of btns) {
					let t = (btn.textContent || '').trim();
					if (t === '删除' && btn.offsetParent !== null) {
						btn.click();
						return 'clicked delete';
					}
				}
				// 没找到删除，找更多/操作
				for (let btn of btns) {
					let t = (btn.textContent || '').trim();
					if ((t === '更多' || t === '⋮' || t === '···') && btn.offsetParent !== null) {
						btn.click();
						return 'clicked more';
					}
				}
			}
		}
		return 'no matching card found';
	}`, articleID)
	if err != nil {
		log.Warnf("查找文章卡片失败: %v", err)
	}
	log.Infof("查找结果: %v", result)
	time.Sleep(2 * time.Second)

	// 如果点了更多，等待菜单弹出后再点删除
	page.Eval(`() => {
		let btns = document.querySelectorAll('span, button, a');
		for (let btn of btns) {
			let t = (btn.textContent || '').trim();
			if (t === '删除' && btn.offsetParent !== null) {
				btn.click();
				return 'clicked delete after more';
			}
		}
		return 'no delete after more';
	}`)
	time.Sleep(2 * time.Second)

	// 确认弹窗
	page.Eval(`() => {
		let btns = document.querySelectorAll('button, span');
		for (let btn of btns) {
			let t = (btn.textContent || '').trim();
			if (t === '确认' || t === '确定' || t.includes('确认删除')) {
				btn.click();
				return 'confirmed';
			}
		}
		return 'no confirm';
	}`)
	time.Sleep(2 * time.Second)

	log.Infof("草稿删除操作完成（结果需通过列表确认）")
	return nil
}
