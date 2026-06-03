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
	ArticleID       string      `json:"article_id"`
	ID              string      `json:"id"`
	Title           string      `json:"title"`
	Status          interface{} `json:"status"`
	CreateTime      interface{} `json:"create_time"`
	ReadCount       int         `json:"go_detail_count_v2"`
	CommentCount    int         `json:"comment_count"`
	DiggCount       int         `json:"digg_count"`
	ImpressionCount int         `json:"impression_count"`
	ArticleURL      string      `json:"article_url"`
}

// ArticleStatusIsDraft 判断头条列表项是否仍处于草稿状态。
func ArticleStatusIsDraft(status interface{}) bool {
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

// ArticleStatusIsPublished 判断头条列表项是否为发布成功后的非草稿状态。
func ArticleStatusIsPublished(status interface{}) bool {
	switch v := status.(type) {
	case float64:
		return int(v) == 3 || int(v) == 6
	case int:
		return v == 3 || v == 6
	case int64:
		return v == 3 || v == 6
	case string:
		normalized := strings.TrimSpace(strings.ToLower(v))
		return normalized == "3" || normalized == "6" || normalized == "published" || normalized == "已发布" || normalized == "审核中" || normalized == "已提交"
	default:
		return false
	}
}

func articleStatusMatchesFilter(status interface{}, filter string) bool {
	switch strings.TrimSpace(strings.ToLower(filter)) {
	case "", "all":
		return true
	case "draft":
		return ArticleStatusIsDraft(status)
	case "published":
		return ArticleStatusIsPublished(status)
	case "review":
		return !ArticleStatusIsDraft(status) && !ArticleStatusIsPublished(status)
	default:
		return true
	}
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

	// 设置 article_id（优先用 id 字段，回退到 pgc_id），并对头条偶发忽略 status 参数的返回做客户端兜底过滤。
	filtered := make([]ArticleItem, 0, len(resp.Data.Articles))
	for i := range resp.Data.Articles {
		if resp.Data.Articles[i].ID != "" {
			resp.Data.Articles[i].ArticleID = resp.Data.Articles[i].ID
		}
		if articleStatusMatchesFilter(resp.Data.Articles[i].Status, params.Status) {
			filtered = append(filtered, resp.Data.Articles[i])
		}
	}

	total := resp.Data.Total
	if strings.TrimSpace(strings.ToLower(params.Status)) != "all" {
		total = len(filtered)
	}

	return &ArticleListResponse{
		Articles: filtered,
		Total:    total,
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
	return DeleteDraftByBrowserWithTitle(ctx, page, articleID, "")
}

// DeleteDraftByBrowserWithTitle 用浏览器删除草稿，支持通过文章标题定位列表卡片。
func DeleteDraftByBrowserWithTitle(ctx context.Context, page *rod.Page, articleID string, articleTitle string) error {
	log.Infof("正在用浏览器删除草稿: %s 标题: %s", articleID, articleTitle)

	// 导航到头条号主页，确保 cookie 生效
	if err := page.Navigate("https://mp.toutiao.com"); err != nil {
		return fmt.Errorf("导航到头条主页失败: %w", err)
	}
	_ = page.Timeout(10 * time.Second).WaitLoad()
	time.Sleep(3 * time.Second)

	draftURLs := []string{
		"https://mp.toutiao.com/profile_v4/graphic/articles?status=draft",
		"https://mp.toutiao.com/profile_v4/graphic/articles?status=1",
		"https://mp.toutiao.com/profile_v4/graphic/articles",
		"https://mp.toutiao.com/profile_v4/graphic/list?status=draft",
		"https://mp.toutiao.com/profile_v4/graphic/list?status=1",
		"https://mp.toutiao.com/profile_v4/graphic/manage?status=draft",
		"https://mp.toutiao.com/profile_v4/graphic/manage?status=1",
	}

	var resultStr string
	for _, draftURL := range draftURLs {
		if err := page.Navigate(draftURL); err != nil {
			log.Warnf("导航到草稿列表页失败 %s: %v", draftURL, err)
			continue
		}
		_ = page.Timeout(10 * time.Second).WaitLoad()
		time.Sleep(4 * time.Second)

		url, _ := page.Eval(`() => window.location.href`)
		title, _ := page.Eval(`() => document.title`)
		log.Infof("当前页面URL: %v, 标题: %v", url, title)
		if navInfo, errNav := clickDraftNavigation(page); errNav == nil && navInfo != "" {
			log.Infof("草稿箱导航点击结果: %s", navInfo)
			time.Sleep(4 * time.Second)
		}
		log.Info("尝试定位并点击删除按钮...")

		var err error
		resultStr, err = clickDraftDeleteOnCurrentPage(page, articleID, articleTitle)
		if err != nil {
			log.Warnf("查找文章卡片失败: %v", err)
			continue
		}
		log.Infof("查找结果: %s", resultStr)
		if resultStr != "" && !strings.Contains(resultStr, "no matching") {
			break
		}
	}
	if resultStr == "" || strings.Contains(resultStr, "no matching") {
		safeScreenshot(page, "./screenshot_delete_draft_not_found.png")
		return fmt.Errorf("未在草稿列表中找到待删除文章: id=%s title=%s，已保存截图 screenshot_delete_draft_not_found.png", articleID, articleTitle)
	}
	time.Sleep(2 * time.Second)

	// 如果点了更多，等待菜单弹出后再点删除。直接点到删除时不要再全页面扫，防止误点下一篇。
	menuResult := ""
	if strings.Contains(resultStr, "clicked more") {
		menuRes, _ := page.Eval(`() => {
			const visible = (el) => {
				if (!el) return false;
				const style = window.getComputedStyle(el);
				const rect = el.getBoundingClientRect();
				return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
			};
			const normalize = (s) => String(s || '').replace(/\s+/g, '').trim();
			const clickLikeUser = (el) => {
				['pointerdown', 'mousedown', 'pointerup', 'mouseup', 'click'].forEach(name => {
					el.dispatchEvent(new MouseEvent(name, { bubbles: true, cancelable: true, view: window }));
				});
			};
			const roots = Array.from(document.querySelectorAll('.byte-popover, .semi-popover, [class*="popover"], [class*="dropdown"], [class*="menu"], [role="menu"], [role="listbox"]')).filter(visible);
			for (const root of roots) {
				const items = Array.from(root.querySelectorAll('li, button, span, a, div, [role="menuitem"], [role="button"]')).filter(visible);
				for (const item of items) {
					const text = normalize(item.textContent);
					if (text.includes('删除') && text.length <= 12) {
						const target = item.closest('li, button, a, [role="menuitem"], [role="button"]') || item;
						clickLikeUser(target);
						return 'clicked delete after more';
					}
				}
			}
			const btns = Array.from(document.querySelectorAll('li, button, span, a, div, [role="menuitem"], [role="button"]')).filter(visible);
			for (const btn of btns) {
				const text = normalize(btn.textContent);
				if (text.includes('删除') && text.length <= 12) {
					clickLikeUser(btn);
					return 'clicked delete after more fallback';
				}
			}
			return 'no delete after more';
		}`)
		if menuRes != nil {
			menuResult = menuRes.Value.Str()
		}
	}
	time.Sleep(2 * time.Second)

	// 确认弹窗
	confirmRes, _ := page.Eval(`() => {
		const visible = (el) => {
			if (!el) return false;
			const style = window.getComputedStyle(el);
			const rect = el.getBoundingClientRect();
			return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
		};
		const normalize = (s) => String(s || '').replace(/\s+/g, '').trim();
		const clickLikeUser = (el) => {
			['pointerdown', 'mousedown', 'pointerup', 'mouseup', 'click'].forEach(name => {
				el.dispatchEvent(new MouseEvent(name, { bubbles: true, cancelable: true, view: window }));
			});
		};
		const roots = Array.from(document.querySelectorAll('.byte-modal, .semi-modal, [role="dialog"], [class*="modal"], [class*="dialog"]')).filter(visible);
		const candidates = [];
		for (const root of roots) {
			candidates.push(...Array.from(root.querySelectorAll('button, span, a, div, [role="button"]')).filter(visible));
		}
		if (candidates.length === 0) {
			candidates.push(...Array.from(document.querySelectorAll('button, span, a, div, [role="button"]')).filter(visible));
		}
		for (const btn of candidates) {
			const text = normalize(btn.textContent);
			if ((text === '删除' || text === '确认' || text === '确定' || text.includes('确认删除') || text.includes('确定删除')) && !text.includes('取消')) {
				const target = btn.closest('button, a, [role="button"]') || btn;
				clickLikeUser(target);
				return 'confirmed';
			}
		}
		return 'no confirm';
	}`)
	time.Sleep(2 * time.Second)
	if menuResult != "" {
		log.Infof("删除菜单点击结果: %s", menuResult)
	}
	if strings.Contains(resultStr, "clicked more") && !strings.Contains(menuResult, "clicked delete") {
		safeScreenshot(page, "./screenshot_delete_draft_menu_error.png")
		return fmt.Errorf("草稿更多菜单中未找到删除按钮: id=%s title=%s result=%s，已保存截图 screenshot_delete_draft_menu_error.png", articleID, articleTitle, menuResult)
	}
	if confirmRes != nil {
		log.Infof("删除确认点击结果: %s", confirmRes.Value.Str())
	}
	if confirmRes == nil || strings.Contains(confirmRes.Value.Str(), "no confirm") {
		safeScreenshot(page, "./screenshot_delete_draft_confirm_error.png")
		return fmt.Errorf("草稿删除确认弹窗未确认: id=%s title=%s，已保存截图 screenshot_delete_draft_confirm_error.png", articleID, articleTitle)
	}

	_ = page.Reload()
	_ = page.Timeout(10 * time.Second).WaitLoad()
	time.Sleep(3 * time.Second)
	existsRes, _ := page.Eval(`(articleID, articleTitle) => {
		const html = document.body ? document.body.innerHTML || '' : '';
		const text = document.body ? document.body.innerText || '' : '';
		return (articleID && (html.includes(articleID) || text.includes(articleID))) ||
			(articleTitle && text.includes(articleTitle));
	}`, articleID, articleTitle)
	if existsRes != nil && existsRes.Value.Bool() {
		return fmt.Errorf("草稿删除后复核仍存在: id=%s title=%s", articleID, articleTitle)
	}

	log.Infof("草稿删除操作完成并通过列表复核")
	return nil
}

func clickDraftNavigation(page *rod.Page) (string, error) {
	result, err := page.Eval(`() => {
		const visible = (el) => {
			if (!el) return false;
			const style = window.getComputedStyle(el);
			const rect = el.getBoundingClientRect();
			return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
		};
		const normalize = (s) => String(s || '').replace(/\s+/g, '').trim();
		const candidates = Array.from(document.querySelectorAll('a, span, div, li, button, [role="button"]')).filter(el => {
			const text = normalize(el.textContent);
			return visible(el) && (text === '草稿箱' || text.includes('草稿箱'));
		});
		for (const el of candidates) {
			el.scrollIntoView({ block: 'center', inline: 'center' });
			el.click();
			return 'clicked draft navigation';
		}
		return 'draft navigation not found';
	}`)
	if err != nil {
		return "", err
	}
	if result == nil {
		return "", nil
	}
	return result.Value.Str(), nil
}

func clickDraftDeleteOnCurrentPage(page *rod.Page, articleID, articleTitle string) (string, error) {
	result, err := page.Eval(`(articleID, articleTitle) => {
		const visible = (el) => {
			if (!el) return false;
			const style = window.getComputedStyle(el);
			const rect = el.getBoundingClientRect();
			return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
		};
		const normalize = (s) => String(s || '').replace(/\s+/g, '').replace(/…|\.{3}/g, '').trim();
		const title = normalize(articleTitle);
		const titlePrefix = title.length > 10 ? title.slice(0, 10) : title;
		const titleLoose = title.length > 6 ? title.slice(0, 6) : title;
		const matches = (el) => {
			const html = normalize(el.innerHTML || '');
			const text = normalize(el.textContent || '');
			return (articleID && (html.includes(articleID) || text.includes(articleID))) ||
				(title && text.includes(title)) ||
				(titlePrefix && text.includes(titlePrefix)) ||
				(titleLoose && text.includes(titleLoose));
		};
		const leafSelectors = 'a, span, div, p, h1, h2, h3, td, [href], [class*="title"]';
		const leaves = Array.from(document.querySelectorAll(leafSelectors)).filter(el => visible(el) && matches(el));
		const cards = [];
		for (const leaf of leaves) {
			let cur = leaf;
			for (let depth = 0; cur && depth < 10; depth++, cur = cur.parentElement) {
				const cls = String(cur.className || '').toLowerCase();
				const text = normalize(cur.textContent || '');
				if (cur.tagName === 'TR' || cur.tagName === 'LI' || cls.includes('item') || cls.includes('card') ||
					cls.includes('article') || cls.includes('work') || cls.includes('list') || text.includes('编辑')) {
					if (!cards.includes(cur)) cards.push(cur);
					break;
				}
			}
		}
		for (const card of Array.from(document.querySelectorAll('div[class*="item"], div[class*="card"], div[class*="article"], div[class*="work"], li[class*="item"], tr, article'))) {
			if (visible(card) && matches(card) && !cards.includes(card)) cards.push(card);
		}
		for (const card of cards) {
			card.scrollIntoView({ block: 'center', inline: 'center' });
			['pointerover', 'mouseover', 'mouseenter'].forEach(name => {
				card.dispatchEvent(new MouseEvent(name, { bubbles: true, cancelable: true, view: window }));
			});
			const btns = Array.from(card.querySelectorAll('button, span, a, div, [role="button"]')).filter(visible);
			const deleteBtn = btns.find(btn => normalize(btn.textContent) === '删除' || normalize(btn.getAttribute('aria-label')).includes('删除') || String(btn.className || '').includes('delete'));
			if (deleteBtn) {
				deleteBtn.click();
				return 'clicked delete';
			}
			const moreBtn = btns.find(btn => {
				const text = normalize(btn.textContent);
				const cls = String(btn.className || '').toLowerCase();
				const aria = normalize(btn.getAttribute('aria-label'));
				return text === '更多' || text === '···' || text === '...' || text.includes('更多') || aria.includes('更多') ||
					cls.includes('more') || cls.includes('operation') || cls.includes('action');
			});
			if (moreBtn) {
				moreBtn.click();
				return 'clicked more';
			}
		}
		return 'no matching card found';
	}`, articleID, articleTitle)
	if err != nil {
		return "", err
	}
	if result == nil {
		return "", nil
	}
	return result.Value.Str(), nil
}
