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
	"github.com/go-rod/rod/lib/proto"
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

	data, err := cookieStore.LoadCookies()
	if err != nil || data == nil {
		return fmt.Errorf("no cookies available, please login first")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	url := configs.DeleteArticleAPI
	payloads := []string{
		fmt.Sprintf(`{"article_id":"%s"}`, articleID),
		fmt.Sprintf(`{"group_id":"%s"}`, articleID),
		fmt.Sprintf(`{"item_id":"%s"}`, articleID),
		fmt.Sprintf(`{"pgc_id":"%s"}`, articleID),
	}
	if articleID != "" && strings.Trim(articleID, "0123456789") == "" {
		payloads = append(payloads,
			fmt.Sprintf(`{"article_id":%s}`, articleID),
			fmt.Sprintf(`{"group_id":%s}`, articleID),
			fmt.Sprintf(`{"item_id":%s}`, articleID),
			fmt.Sprintf(`{"pgc_id":%s}`, articleID),
		)
	}

	var lastErr error
	for _, body := range payloads {
		req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		injectCookies(req, data)

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("delete request failed with payload %s: %w", body, err)
			continue
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("delete failed with payload %s status %d: %s", body, resp.StatusCode, string(respBody))
			continue
		}

		// 头条旧删除 API 已可能废弃，常见返回 code=20007124 Id invalid；继续换参数尝试。
		var apiResp struct {
			Code int `json:"code"`
		}
		if err := json.Unmarshal(respBody, &apiResp); err == nil && apiResp.Code != 0 {
			lastErr = fmt.Errorf("delete API returned error code %d with payload %s: %s", apiResp.Code, body, string(respBody))
			log.Warnf("删除 API 参数尝试失败: %v", lastErr)
			continue
		}

		log.Infof("删除 API 参数尝试成功: %s", body)
		return nil
	}

	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("delete API failed: no payload attempted")
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

// DeleteDraftByBrowserOnPage 在当前已加载的草稿列表页面上删除草稿，不重新触发 SPA 导航。
func DeleteDraftByBrowserOnPage(ctx context.Context, page *rod.Page, articleID string, articleTitle string) error {
	log.Infof("正在当前页面删除草稿: %s 标题: %s", articleID, articleTitle)

	url, _ := page.Eval(`() => window.location.href`)
	currentURL := ""
	if url != nil {
		currentURL = url.Value.Str()
	}
	log.Infof("当前页面URL: %s", currentURL)
	if strings.Contains(currentURL, "login") {
		return fmt.Errorf("跳转到登录页，Cookie可能过期")
	}

	_, err := deleteDraftFromCurrentPage(page, articleID, articleTitle)
	return err
}

// DeleteDraftByBrowserWithTitle 用浏览器删除草稿，支持通过文章标题定位列表卡片。
func DeleteDraftByBrowserWithTitle(ctx context.Context, page *rod.Page, articleID string, articleTitle string) error {
	log.Infof("正在用浏览器删除草稿: %s 标题: %s", articleID, articleTitle)

	draftURLs := []string{
		"https://mp.toutiao.com/profile_v4/graphic/articles?status=draft",
		"https://mp.toutiao.com/profile_v4/graphic/articles?status=1",
		"https://mp.toutiao.com/profile_v4/graphic/articles",
	}

	var resultStr string
	var lastErr error
	deleted := false
	for _, draftURL := range draftURLs {
		if err := page.Navigate(draftURL); err != nil {
			log.Warnf("导航到草稿列表页失败 %s: %v", draftURL, err)
			lastErr = err
			continue
		}
		_ = page.Timeout(10 * time.Second).WaitLoad()
		time.Sleep(4 * time.Second)

		url, _ := page.Eval(`() => window.location.href`)
		title, _ := page.Eval(`() => document.title`)
		log.Infof("当前页面URL: %v, 标题: %v", url, title)
		currentURL := ""
		if url != nil {
			currentURL = url.Value.Str()
		}
		if strings.Contains(currentURL, "login") {
			return fmt.Errorf("跳转到登录页，Cookie可能过期")
		}

		navClicked := false
		if shouldClickDraftNavigation(currentURL, draftURL) {
			if navInfo, errNav := clickDraftNavigation(page); errNav == nil && navInfo != "" {
				log.Infof("草稿箱导航点击结果: %s", navInfo)
				time.Sleep(4 * time.Second)
				navClicked = strings.Contains(navInfo, "clicked")
			}
		} else {
			log.Infof("当前 URL 已指向草稿列表，跳过左侧草稿箱导航点击: %s", currentURL)
		}
		if navClicked {
			// 导航点击后刷新一次当前 URL，便于日志定位 SPA 路由变化。
			urlAfterNav, _ := page.Eval(`() => window.location.href`)
			navURL := ""
			if urlAfterNav != nil {
				navURL = urlAfterNav.Value.Str()
			}
			log.Infof("草稿箱导航后 URL: %s", navURL)
		}
		var err error
		resultStr, err = deleteDraftFromCurrentPage(page, articleID, articleTitle)
		if err != nil {
			log.Warnf("当前草稿列表页删除失败: %v", err)
			lastErr = err
			continue
		}
		if resultStr != "" {
			deleted = true
			break
		}
	}
	if !deleted {
		if lastErr != nil {
			return lastErr
		}
	}
	if resultStr == "" || strings.Contains(resultStr, "no matching") || strings.Contains(resultStr, "no checkbox") {
		safeScreenshot(page, "./screenshot_delete_draft_not_found.png")
		return fmt.Errorf("未在草稿列表中找到待删除文章: id=%s title=%s result=%s，已保存截图 screenshot_delete_draft_not_found.png", articleID, articleTitle, resultStr)
	}
	return nil
}

func deleteDraftFromCurrentPage(page *rod.Page, articleID string, articleTitle string) (string, error) {
	_, _ = page.Eval(`() => {
		window.scrollTo(0, window.scrollY || 0);
		document.scrollingElement && (document.scrollingElement.scrollLeft = 0);
		document.querySelectorAll('*').forEach(el => {
			if (el.scrollLeft) el.scrollLeft = 0;
		});
	}`)

	log.Info("尝试定位目标草稿并勾选复选框...")
	resultStr, err := clickDraftCheckboxWithRetry(page, articleID, articleTitle)
	if err != nil {
		return resultStr, fmt.Errorf("草稿复选框点击失败: %w", err)
	}
	log.Infof("草稿复选框定位结果: %s", resultStr)
	if resultStr == "" || strings.Contains(resultStr, "no matching") {
		safeScreenshot(page, "./screenshot_delete_draft_not_found.png")
		return resultStr, fmt.Errorf("未在草稿列表中找到待删除文章: id=%s title=%s result=%s，已保存截图 screenshot_delete_draft_not_found.png", articleID, articleTitle, resultStr)
	}
	if strings.Contains(resultStr, "no checkbox") {
		safeScreenshot(page, "./screenshot_delete_draft_checkbox_error.png")
		return resultStr, fmt.Errorf("已找到草稿但未找到可勾选复选框: id=%s title=%s result=%s，已保存截图 screenshot_delete_draft_checkbox_error.png", articleID, articleTitle, resultStr)
	}

	time.Sleep(1500 * time.Millisecond)
	batchResult, err := clickDraftBatchDeleteOnCurrentPage(page)
	if err != nil {
		safeScreenshot(page, "./screenshot_delete_draft_batch_error.png")
		return resultStr, fmt.Errorf("草稿批量删除按钮点击失败: id=%s title=%s err=%v，已保存截图 screenshot_delete_draft_batch_error.png", articleID, articleTitle, err)
	}
	log.Infof("草稿批量删除点击结果: %s", batchResult)
	if batchResult == "" || strings.Contains(batchResult, "no batch delete") {
		safeScreenshot(page, "./screenshot_delete_draft_batch_error.png")
		return resultStr, fmt.Errorf("勾选草稿后未找到批量删除按钮: id=%s title=%s result=%s，已保存截图 screenshot_delete_draft_batch_error.png", articleID, articleTitle, batchResult)
	}

	time.Sleep(2 * time.Second)
	confirmResult, err := clickDraftDeleteConfirm(page)
	if err != nil {
		safeScreenshot(page, "./screenshot_delete_draft_confirm_error.png")
		return resultStr, fmt.Errorf("草稿删除确认弹窗点击失败: id=%s title=%s err=%v，已保存截图 screenshot_delete_draft_confirm_error.png", articleID, articleTitle, err)
	}
	log.Infof("草稿删除确认点击结果: %s", confirmResult)
	if confirmResult == "" || strings.Contains(confirmResult, "no confirm") {
		safeScreenshot(page, "./screenshot_delete_draft_confirm_error.png")
		return resultStr, fmt.Errorf("草稿删除确认弹窗未确认: id=%s title=%s result=%s，已保存截图 screenshot_delete_draft_confirm_error.png", articleID, articleTitle, confirmResult)
	}

	_ = page.Reload()
	_ = page.Timeout(10 * time.Second).WaitLoad()
	time.Sleep(3 * time.Second)
	log.Infof("草稿删除操作已提交，等待 API 列表复核: %s", articleID)
	return resultStr, nil
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

func shouldClickDraftNavigation(currentURL, requestedURL string) bool {
	combined := strings.ToLower(currentURL + " " + requestedURL)
	return !strings.Contains(combined, "status=draft") && !strings.Contains(combined, "status=1")
}

func clickDraftCheckboxWithRetry(page *rod.Page, articleID, articleTitle string) (string, error) {
	var lastResult string
	var lastErr error
	for attempt := 1; attempt <= 5; attempt++ {
		result, err := clickDraftCheckboxOnCurrentPage(page, articleID, articleTitle)
		if err != nil {
			lastErr = err
			log.Warnf("第 %d 次定位草稿复选框失败: %v", attempt, err)
			time.Sleep(1500 * time.Millisecond)
			continue
		}
		lastResult = result
		if result != "" && !strings.Contains(result, "no matching") && !strings.Contains(result, "no checkbox") {
			if attempt > 1 {
				log.Infof("第 %d 次等待后成功定位草稿复选框: %s", attempt, result)
			}
			return result, nil
		}
		log.Infof("第 %d 次定位草稿复选框未命中: %s", attempt, result)
		time.Sleep(1500 * time.Millisecond)
	}
	if lastErr != nil && lastResult == "" {
		return "", lastErr
	}
	return lastResult, nil
}

func clickDraftCheckboxOnCurrentPage(page *rod.Page, articleID, articleTitle string) (string, error) {
	result, err := page.Eval(`(articleID, articleTitle) => {
		document.querySelectorAll('.mcp-draft-checkbox').forEach(el => {
			el.classList.remove('mcp-draft-checkbox');
		});
		const visible = (el) => {
			if (!el) return false;
			const style = window.getComputedStyle(el);
			const rect = el.getBoundingClientRect();
			return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
		};
		const normalize = (s) => String(s || '').replace(/\s+/g, '').replace(/…|\.{3}/g, '').trim();
		const checked = (el) => {
			const input = el.matches && el.matches('input[type="checkbox"]') ? el : el.querySelector && el.querySelector('input[type="checkbox"]');
			return (input && input.checked) || el.getAttribute('aria-checked') === 'true' || /\bchecked\b/.test(String(el.className || '').toLowerCase());
		};
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
		const interactiveCheckbox = (el) => {
			if (!el) return null;
			const direct = el.matches && (el.matches('input[type="checkbox"]') || el.getAttribute('role') === 'checkbox' || String(el.className || '').toLowerCase().includes('checkbox')) ? el : null;
			const found = direct || el.querySelector('input[type="checkbox"], [role="checkbox"], [class*="checkbox"]');
			if (!found) return null;
			const target = found.closest('label, button, [role="checkbox"], [class*="checkbox"]') || found;
			return visible(target) ? target : null;
		};
		const leafSelectors = 'a, span, div, p, h1, h2, h3, td, [href], [class*="title"]';
		const leaves = Array.from(document.querySelectorAll(leafSelectors)).filter(el => visible(el) && matches(el));
		const cards = [];
		for (const leaf of leaves) {
			let cur = leaf;
			for (let depth = 0; cur && depth < 12; depth++, cur = cur.parentElement) {
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
			const inCard = interactiveCheckbox(card);
			if (inCard) {
				if (checked(inCard)) return 'checkbox already checked';
				inCard.classList.add('mcp-draft-checkbox');
				return 'marked checkbox';
			}
			const parent = card.parentElement;
			const siblings = parent ? Array.from(parent.children) : [];
			for (const sibling of siblings) {
				if (!visible(sibling)) continue;
				if (normalize(sibling.textContent || '').includes(titleLoose) || sibling === card) {
					const cb = interactiveCheckbox(sibling);
					if (cb) {
						if (checked(cb)) return 'checkbox already checked';
						cb.classList.add('mcp-draft-checkbox');
						return 'marked checkbox sibling';
					}
				}
			}
		}
		if (cards.length > 0) {
			const diagnostics = cards.map(card => normalize(card.textContent || '').slice(0, 80)).slice(0, 3);
			return 'no checkbox in matching card; cards=' + JSON.stringify(diagnostics);
		}
		return 'no matching card found';
	}`, articleID, articleTitle)
	if err != nil {
		return "", err
	}
	if result == nil {
		return "", nil
	}
	resultStr := result.Value.Str()
	switch resultStr {
	case "marked checkbox", "marked checkbox sibling":
		if err := physicalClickMarkedElement(page, ".mcp-draft-checkbox"); err != nil {
			return resultStr, err
		}
		return "clicked checkbox", nil
	default:
		return resultStr, nil
	}
}

func clickDraftBatchDeleteOnCurrentPage(page *rod.Page) (string, error) {
	result, err := page.Eval(`() => {
		document.querySelectorAll('.mcp-draft-batch-delete').forEach(el => el.classList.remove('mcp-draft-batch-delete'));
		const visible = (el) => {
			if (!el) return false;
			const style = window.getComputedStyle(el);
			const rect = el.getBoundingClientRect();
			return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
		};
		const normalize = (s) => String(s || '').replace(/\s+/g, '').trim();
		const roots = Array.from(document.querySelectorAll('[class*="batch"], [class*="toolbar"], [class*="operation"], [class*="action"], .byte-table, .semi-table, body')).filter(visible);
		const candidates = [];
		for (const root of roots) {
			candidates.push(...Array.from(root.querySelectorAll('button, a, span, div, [role="button"]')).filter(visible));
		}
		const unique = [...new Set(candidates)];
		for (const el of unique) {
			const text = normalize(el.textContent || el.getAttribute('aria-label') || el.getAttribute('title'));
			const cls = String(el.className || '').toLowerCase();
			if ((text === '删除' || text === '批量删除' || text === '删除草稿' || cls.includes('delete')) && !text.includes('取消')) {
				const target = el.closest('button, a, [role="button"], [class*="button"]') || el;
				target.scrollIntoView({ block: 'center', inline: 'center' });
				target.classList.add('mcp-draft-batch-delete');
				return 'marked batch delete: ' + text;
			}
		}
		const texts = unique.map(el => normalize(el.textContent || el.getAttribute('aria-label') || el.getAttribute('title'))).filter(text => text && text.length <= 24).slice(0, 50);
		return 'no batch delete; texts=' + JSON.stringify(texts);
	}`)
	if err != nil {
		return "", err
	}
	if result == nil {
		return "", nil
	}
	resultStr := result.Value.Str()
	if strings.Contains(resultStr, "marked batch delete") {
		if err := physicalClickMarkedElement(page, ".mcp-draft-batch-delete"); err != nil {
			return resultStr, err
		}
		return "clicked batch delete", nil
	}
	return resultStr, nil
}

func clickDraftDeleteConfirm(page *rod.Page) (string, error) {
	result, err := page.Eval(`() => {
		document.querySelectorAll('.mcp-draft-confirm-delete').forEach(el => el.classList.remove('mcp-draft-confirm-delete'));
		const visible = (el) => {
			if (!el) return false;
			const style = window.getComputedStyle(el);
			const rect = el.getBoundingClientRect();
			return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
		};
		const normalize = (s) => String(s || '').replace(/\s+/g, '').trim();
		const roots = Array.from(document.querySelectorAll('.byte-modal, .semi-modal, [role="dialog"], [class*="modal"], [class*="dialog"], body')).filter(visible);
		for (const root of roots) {
			const candidates = Array.from(root.querySelectorAll('button, a, span, div, [role="button"]')).filter(visible);
			for (const el of candidates) {
				const text = normalize(el.textContent || el.getAttribute('aria-label') || el.getAttribute('title'));
				if ((text === '删除' || text === '确认' || text === '确定' || text === '确认删除' || text === '确定删除') && !text.includes('取消')) {
					const target = el.closest('button, a, [role="button"], [class*="button"]') || el;
					target.scrollIntoView({ block: 'center', inline: 'center' });
					target.classList.add('mcp-draft-confirm-delete');
					return 'marked confirm: ' + text;
				}
			}
		}
		return 'no confirm';
	}`)
	if err != nil {
		return "", err
	}
	if result == nil {
		return "", nil
	}
	resultStr := result.Value.Str()
	if strings.Contains(resultStr, "marked confirm") {
		if err := physicalClickMarkedElement(page, ".mcp-draft-confirm-delete"); err != nil {
			return resultStr, err
		}
		return "clicked confirm", nil
	}
	return resultStr, nil
}

func physicalClickMarkedElement(page *rod.Page, selector string) error {
	el, err := page.Timeout(3 * time.Second).Element(selector)
	if err != nil {
		return fmt.Errorf("定位标记元素失败 %s: %w", selector, err)
	}
	_, _ = el.Eval(`() => {
		this.scrollIntoView({ block: 'center', inline: 'center' });
		let p = this.parentElement;
		while (p) {
			if (p.scrollLeft) {
				const rect = this.getBoundingClientRect();
				const parentRect = p.getBoundingClientRect();
				if (rect.left < parentRect.left || rect.right > parentRect.right) {
					p.scrollLeft += rect.left - parentRect.left - Math.max(24, parentRect.width / 2);
				}
			}
			p = p.parentElement;
		}
	}`)
	time.Sleep(300 * time.Millisecond)
	pt, err := el.Interactable()
	if err != nil {
		log.Warnf("标记元素无法物理点击，回退 JS 点击 %s: %v", selector, err)
		_, jsErr := el.Eval(`() => {
			['pointerover', 'mouseover', 'mouseenter', 'pointerdown', 'mousedown', 'pointerup', 'mouseup', 'click'].forEach(name => {
				this.dispatchEvent(new MouseEvent(name, { bubbles: true, cancelable: true, view: window }));
			});
		}`)
		return jsErr
	}
	log.Infof("物理点击草稿操作按钮 %s，坐标 (%f, %f)", selector, pt.X, pt.Y)
	_ = page.Mouse.MoveTo(*pt)
	_ = page.Mouse.Down(proto.InputMouseButtonLeft, 1)
	_ = page.Mouse.Up(proto.InputMouseButtonLeft, 1)
	return nil
}
