package toutiaohao

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/example/toutiaohao-mcp-server/cookies"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	log "github.com/sirupsen/logrus"
)

// CommentListParams 评论列表查询参数。
type CommentListParams struct {
	ArticleID string `json:"article_id,omitempty"`
	Keyword   string `json:"keyword,omitempty"`
	PageSize  int    `json:"page_size,omitempty"`
}

// CommentItem 对外暴露的评论条目。
type CommentItem struct {
	CommentID    string `json:"comment_id"`
	ArticleID    string `json:"article_id,omitempty"`
	UserName     string `json:"user_name,omitempty"`
	Content      string `json:"content"`
	ReplyContent string `json:"reply_content,omitempty"`
	ReplyCount   int    `json:"reply_count"`
	CreateTime   string `json:"create_time,omitempty"`
	RawText      string `json:"raw_text,omitempty"`
}

// CommentListResponse 评论列表响应。
type CommentListResponse struct {
	Comments   []CommentItem `json:"comments"`
	Total      int           `json:"total"`
	Diagnostic string        `json:"diagnostic,omitempty"`
}

// CommentProbeParams 评论管理页诊断参数。
type CommentProbeParams struct {
	ArticleID string `json:"article_id,omitempty"`
	WaitMS    int    `json:"wait_ms,omitempty"`
}

// CommentProbeResponse 评论管理页诊断响应。
type CommentProbeResponse struct {
	Status          string   `json:"status"`
	OK              bool     `json:"ok"`
	URL             string   `json:"url"`
	BodyTextLen     int      `json:"body_text_len"`
	NodeCount       int      `json:"node_count"`
	ActionCount     int      `json:"action_count"`
	EmptyState      bool     `json:"empty_state"`
	ResourceSamples []string `json:"resource_samples,omitempty"`
	Reason          string   `json:"reason,omitempty"`
	OpenError       string   `json:"open_error,omitempty"`
}

// ReplyCommentResult 回复评论结果。
type ReplyCommentResult struct {
	Success          bool   `json:"success"`
	Message          string `json:"message"`
	ArticleID        string `json:"article_id,omitempty"`
	CommentID        string `json:"comment_id,omitempty"`
	CommentText      string `json:"comment_text,omitempty"`
	ReplyContent     string `json:"reply_content"`
	ReplyCountBefore *int   `json:"reply_count_before,omitempty"`
	ReplyCountAfter  *int   `json:"reply_count_after,omitempty"`
}

// CommentAction 通过头条后台页面进行评论管理。
type CommentAction struct {
	page        *rod.Page
	cookieStore cookies.Cookier
}

// NewCommentAction 创建评论管理动作。
func NewCommentAction(page *rod.Page, cookieStore cookies.Cookier) *CommentAction {
	return &CommentAction{page: page, cookieStore: cookieStore}
}

// NewCommentListParams 创建评论列表参数（含默认值与 MCP 数字类型兼容）。
func NewCommentListParams(args map[string]interface{}) *CommentListParams {
	params := &CommentListParams{PageSize: 20}
	if args == nil {
		return params
	}
	if articleID, ok := args["article_id"].(string); ok {
		params.ArticleID = strings.TrimSpace(articleID)
	}
	if keyword, ok := args["keyword"].(string); ok {
		params.Keyword = strings.TrimSpace(keyword)
	}
	if val, ok := args["page_size"]; ok && val != nil {
		switch v := val.(type) {
		case int:
			params.PageSize = v
		case float64:
			params.PageSize = int(v)
		case string:
			var n int
			if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
				params.PageSize = n
			}
		}
	}
	return params
}

// ValidateGetComments 校验评论列表参数。
func ValidateGetComments(params *CommentListParams) error {
	if params == nil {
		return nil
	}
	if params.PageSize < 0 {
		return fmt.Errorf("page_size 不能为负数")
	}
	return nil
}

// ValidateReplyComment 校验回复评论参数。
func ValidateReplyComment(articleID, commentID, commentText, replyContent string) error {
	if strings.TrimSpace(replyContent) == "" {
		return fmt.Errorf("reply_content 不能为空")
	}
	if strings.TrimSpace(commentID) == "" && strings.TrimSpace(commentText) == "" {
		return fmt.Errorf("comment_id 和 comment_text 至少需要提供一个，用于定位要回复的评论")
	}
	return nil
}

// GetComments 获取评论列表。优先调用评论管理页实际使用的接口，DOM 仅作为兜底。
func (a *CommentAction) GetComments(ctx context.Context, params *CommentListParams) (*CommentListResponse, error) {
	if params == nil {
		params = &CommentListParams{}
	}
	if err := ValidateGetComments(params); err != nil {
		return nil, err
	}
	if params.PageSize <= 0 {
		params.PageSize = 20
	}
	if err := a.openCommentManagePage(ctx, params.ArticleID); err != nil {
		return nil, err
	}
	info, err := getCommentManageSurface(a.page)
	if err != nil {
		return nil, err
	}
	if !info.OK {
		diag := commentSurfaceDiagnosticJSON(info, "评论管理后台未挂载真实评论模块；已停止 DOM 抓取，避免长时间空跑")
		log.Warnf("评论列表不可用: %s", diag)
		return &CommentListResponse{Comments: []CommentItem{}, Total: 0, Diagnostic: diag}, nil
	}
	comments, diagnostic, apiErr := fetchCommentsFromAPI(a.page, params.ArticleID, params.Keyword, params.PageSize)
	if apiErr == nil {
		log.Infof("评论真实接口返回 %d 条", len(comments))
		return &CommentListResponse{Comments: comments, Total: len(comments), Diagnostic: diagnostic}, nil
	}
	log.Warnf("评论真实接口获取失败，回退 DOM 抓取: %v", apiErr)

	comments, diagnostic, err = scrapeCommentsOnCurrentPage(a.page, params.ArticleID, params.Keyword, params.PageSize)
	if err != nil {
		safeScreenshot(a.page, "./screenshot_comments_list_error.png")
		return nil, err
	}
	return &CommentListResponse{Comments: comments, Total: len(comments), Diagnostic: diagnostic}, nil
}

// ProbeCommentManage 只读诊断评论管理页挂载状态，不抓取或回复评论。
func (a *CommentAction) ProbeCommentManage(ctx context.Context, params *CommentProbeParams) (*CommentProbeResponse, error) {
	if params == nil {
		params = &CommentProbeParams{}
	}
	openErr := a.probeCommentManagePage(ctx, params.ArticleID)
	if params.WaitMS > 0 {
		wait := time.Duration(params.WaitMS) * time.Millisecond
		if wait > 5*time.Second {
			wait = 5 * time.Second
		}
		time.Sleep(wait)
	}
	info, err := getCommentManageSurface(a.page)
	if err != nil {
		return nil, err
	}
	resp := &CommentProbeResponse{
		Status:          info.Status,
		OK:              info.OK,
		URL:             info.URL,
		BodyTextLen:     info.BodyTextLen,
		NodeCount:       info.NodeCount,
		ActionCount:     info.ActionCount,
		EmptyState:      info.EmptyState,
		ResourceSamples: info.ResourceSamples,
		Reason:          info.Reason,
	}
	if openErr != nil {
		resp.OpenError = openErr.Error()
	}
	return resp, nil
}

func (a *CommentAction) probeCommentManagePage(ctx context.Context, articleID string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	prepareCommentPage(a.page)
	articleID = strings.TrimSpace(articleID)

	if err := a.page.Navigate("https://mp.toutiao.com/profile_v4/index"); err == nil {
		_ = a.page.Timeout(5 * time.Second).WaitLoad()
		time.Sleep(1 * time.Second)
		_ = activateCommentV2Flag(a.page)
		logCommentProbeSurface(a.page, "首页加载后")
		if navInfo, err := clickCommentManageNavigation(a.page); err == nil && navInfo != "" {
			log.Infof("评论管理短探测首页导航点击结果: %s", navInfo)
			if ok, reason := waitForCommentManageSurface(a.page, 10*time.Second); ok {
				log.Infof("评论管理短探测首页导航后识别成功: %s", reason)
				return nil
			} else {
				log.Warnf("评论管理短探测首页导航后仍未挂载: %s", reason)
				logCommentProbeSurface(a.page, "首页点击评论管理后")
			}
		} else if err != nil {
			log.Warnf("评论管理短探测首页导航点击失败: %v", err)
		}
	} else {
		log.Warnf("评论管理短探测首页导航失败: %v", err)
	}

	url := "https://mp.toutiao.com/profile_v4/comment/manage"
	if articleID != "" {
		url = fmt.Sprintf("%s?group_id=%s", url, articleID)
	}
	if err := a.page.Navigate(url); err != nil {
		return fmt.Errorf("评论管理短探测直达 URL 失败: %w", err)
	}
	_ = a.page.Timeout(5 * time.Second).WaitLoad()
	time.Sleep(1 * time.Second)
	_ = activateCommentV2Flag(a.page)
	info := logCommentProbeSurface(a.page, "直达评论管理页后")
	if info != nil && info.OK {
		return nil
	}
	if navInfo, err := clickCommentManageNavigation(a.page); err == nil && navInfo != "" {
		log.Infof("评论管理短探测直达导航点击结果: %s", navInfo)
		time.Sleep(1500 * time.Millisecond)
		logCommentProbeSurface(a.page, "直达页点击评论管理后")
	} else if err != nil {
		log.Warnf("评论管理短探测直达导航点击失败: %v", err)
	}
	return nil
}

func logCommentProbeSurface(page *rod.Page, stage string) *commentManageSurfaceInfo {
	info, err := getCommentManageSurface(page)
	if err != nil {
		log.Warnf("评论管理短探测状态[%s] 获取失败: %v", stage, err)
		return nil
	}
	log.Infof("评论管理短探测状态[%s]: status=%s ok=%v url=%s body=%d nodes=%d actions=%d empty=%v resources=%s reason=%s",
		stage,
		info.Status,
		info.OK,
		info.URL,
		info.BodyTextLen,
		info.NodeCount,
		info.ActionCount,
		info.EmptyState,
		strings.Join(info.ResourceSamples, "|"),
		info.Reason,
	)
	return info
}

func commentSurfaceDiagnosticJSON(info *commentManageSurfaceInfo, note string) string {
	if info == nil {
		return fmt.Sprintf(`{"note":%q}`, note)
	}
	data, _ := json.Marshal(map[string]interface{}{
		"note":             note,
		"status":           info.Status,
		"ok":               info.OK,
		"url":              info.URL,
		"body_text_len":    info.BodyTextLen,
		"node_count":       info.NodeCount,
		"action_count":     info.ActionCount,
		"empty_state":      info.EmptyState,
		"resource_samples": info.ResourceSamples,
		"reason":           info.Reason,
	})
	return string(data)
}

// ReplyComment 回复指定评论。commentID 与 commentText 二选一，二者都传时会共同约束目标。
func (a *CommentAction) ReplyComment(ctx context.Context, articleID, commentID, commentText, replyContent string) (*ReplyCommentResult, error) {
	articleID = strings.TrimSpace(articleID)
	commentID = strings.TrimSpace(commentID)
	commentText = strings.TrimSpace(commentText)
	replyContent = strings.TrimSpace(replyContent)
	if err := ValidateReplyComment(articleID, commentID, commentText, replyContent); err != nil {
		return nil, err
	}
	if err := a.openCommentManagePage(ctx, articleID); err != nil {
		return nil, err
	}
	info, err := getCommentManageSurface(a.page)
	if err != nil {
		return nil, err
	}
	if !info.OK {
		return nil, fmt.Errorf("评论管理后台未挂载真实评论模块，无法定位和回复评论；诊断: %s", commentSurfaceDiagnosticJSON(info, "reply_comment stopped before DOM action"))
	}
	beforeReplyCount := -1
	if commentID != "" {
		apiItem, lookupErr := findCommentAPIItemByID(a.page, commentID)
		if lookupErr != nil {
			log.Warnf("通过真实接口解析评论 %s 失败，将继续尝试 DOM ID 定位: %v", commentID, lookupErr)
		} else if apiItem != nil {
			item := commentItemFromAPI(*apiItem)
			beforeReplyCount = apiItem.ReplyCount
			if commentText == "" {
				commentText = item.Content
			}
			if articleID == "" {
				articleID = item.ArticleID
			}
			log.Infof("已通过真实接口补齐评论定位信息: comment_id=%s article_id=%s reply_count=%d", commentID, articleID, beforeReplyCount)
		}
	}
	if commentText != "" {
		if err := loadCommentTargetIntoDOM(a.page, commentText, 15*time.Second); err != nil {
			log.Warnf("滚动加载目标评论失败，将在当前 DOM 继续定位: %v", err)
		}
	}
	if err := markCommentReplyButton(a.page, articleID, commentID, commentText); err != nil {
		safeScreenshot(a.page, "./screenshot_comment_reply_not_found.png")
		return nil, err
	}
	if err := physicalClickMarkedElement(a.page, ".mcp-comment-reply-button"); err != nil {
		safeScreenshot(a.page, "./screenshot_comment_reply_click_error.png")
		return nil, fmt.Errorf("点击评论回复按钮失败: %w", err)
	}
	time.Sleep(800 * time.Millisecond)
	if err := fillCommentReplyEditor(a.page, replyContent); err != nil {
		safeScreenshot(a.page, "./screenshot_comment_reply_fill_error.png")
		return nil, err
	}
	if err := clickCommentReplySubmit(a.page); err != nil {
		safeScreenshot(a.page, "./screenshot_comment_reply_submit_error.png")
		return nil, err
	}
	verification, afterReplyCount, err := verifyCommentReplySubmitted(a.page, commentID, commentText, replyContent, beforeReplyCount, 15*time.Second)
	if err != nil {
		safeScreenshot(a.page, "./screenshot_comment_reply_verify_error.png")
		return nil, err
	}
	var beforeCountResult *int
	var afterCountResult *int
	if beforeReplyCount >= 0 {
		beforeCount := beforeReplyCount
		afterCount := afterReplyCount
		beforeCountResult = &beforeCount
		afterCountResult = &afterCount
	}
	return &ReplyCommentResult{
		Success:          true,
		Message:          "评论回复已提交并验证: " + verification,
		ArticleID:        articleID,
		CommentID:        commentID,
		CommentText:      commentText,
		ReplyContent:     replyContent,
		ReplyCountBefore: beforeCountResult,
		ReplyCountAfter:  afterCountResult,
	}, nil
}

func (a *CommentAction) openCommentManagePage(ctx context.Context, articleID string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	prepareCommentPage(a.page)
	articleID = strings.TrimSpace(articleID)
	log.Infof("优先从创作者后台首页点击评论管理入口")
	homeURLs := []string{
		"https://mp.toutiao.com/profile_v4/index",
		"https://mp.toutiao.com/profile_v4",
	}
	for _, url := range homeURLs {
		log.Infof("正在打开头条创作者后台首页: %s", url)
		if err := a.page.Navigate(url); err != nil {
			log.Warnf("评论管理首页导航失败: %s: %v", url, err)
			continue
		}
		_ = a.page.Timeout(15 * time.Second).WaitLoad()
		time.Sleep(2 * time.Second)
		_ = activateCommentV2Flag(a.page)
		if navInfo, err := clickCommentManageNavigation(a.page); err == nil && navInfo != "" {
			log.Infof("评论管理首页导航点击结果: %s", navInfo)
			time.Sleep(4 * time.Second)
			if ok, reason := waitForCommentManageSurface(a.page, 15*time.Second); ok {
				log.Infof("评论管理页首页导航后识别成功: %s", reason)
				return nil
			} else {
				log.Warnf("评论管理页首页导航后仍未挂载: %s", reason)
			}
		} else if err != nil {
			log.Warnf("评论管理首页导航点击失败: %v", err)
		}
	}

	urls := []string{
		"https://mp.toutiao.com/profile_v4/comment/manage",
		"https://mp.toutiao.com/profile_v4/comment/comment_manage",
		"https://mp.toutiao.com/profile_v4/comment",
		"https://mp.toutiao.com/profile_v4/comment/list",
		"https://mp.toutiao.com/profile_v4/comment/reply",
		"https://mp.toutiao.com/profile_v4/interaction/comment",
		"https://mp.toutiao.com/profile_v4/content/comment",
	}
	if articleID != "" {
		urls = append([]string{
			fmt.Sprintf("https://mp.toutiao.com/profile_v4/comment/manage?group_id=%s", articleID),
			fmt.Sprintf("https://mp.toutiao.com/profile_v4/comment/comment_manage?group_id=%s", articleID),
			fmt.Sprintf("https://mp.toutiao.com/profile_v4/comment?group_id=%s", articleID),
			fmt.Sprintf("https://mp.toutiao.com/profile_v4/comment/list?group_id=%s", articleID),
			fmt.Sprintf("https://mp.toutiao.com/profile_v4/comment/reply?group_id=%s", articleID),
		}, urls...)
	}
	var lastURL string
	var lastReason string
	for _, url := range urls {
		lastURL = url
		log.Infof("正在打开头条评论管理页: %s", url)
		if err := a.page.Navigate(url); err != nil {
			log.Warnf("评论管理页导航失败: %s: %v", url, err)
			continue
		}
		_ = a.page.Timeout(15 * time.Second).WaitLoad()
		time.Sleep(3 * time.Second)
		_ = activateCommentV2Flag(a.page)
		if ok, reason := waitForCommentManageSurface(a.page, 10*time.Second); ok {
			log.Infof("评论管理页识别成功: %s", reason)
			return nil
		} else {
			lastReason = reason
			log.Warnf("评论管理页识别失败: %s", reason)
		}
		if navInfo, err := clickCommentManageNavigation(a.page); err == nil && navInfo != "" {
			log.Infof("评论管理导航点击结果: %s", navInfo)
			time.Sleep(4 * time.Second)
			if ok, reason := waitForCommentManageSurface(a.page, 12*time.Second); ok {
				log.Infof("评论管理页导航后识别成功: %s", reason)
				return nil
			} else {
				lastReason = reason
				log.Warnf("评论管理页导航后仍未挂载: %s", reason)
			}
		} else if err != nil {
			log.Warnf("评论管理导航点击失败: %v", err)
		}
	}
	log.Infof("评论直达路由未挂载真实模块，尝试从创作者后台首页点击评论管理")
	for _, url := range homeURLs {
		lastURL = url
		if err := a.page.Navigate(url); err != nil {
			log.Warnf("评论管理首页导航失败: %s: %v", url, err)
			continue
		}
		_ = a.page.Timeout(15 * time.Second).WaitLoad()
		time.Sleep(3 * time.Second)
		_ = activateCommentV2Flag(a.page)
		if navInfo, err := clickCommentManageNavigation(a.page); err == nil && navInfo != "" {
			log.Infof("评论管理首页导航点击结果: %s", navInfo)
			time.Sleep(4 * time.Second)
			if ok, reason := waitForCommentManageSurface(a.page, 15*time.Second); ok {
				log.Infof("评论管理页首页导航后识别成功: %s", reason)
				return nil
			} else {
				lastReason = reason
				log.Warnf("评论管理页首页导航后仍未挂载: %s", reason)
			}
		} else if err != nil {
			log.Warnf("评论管理首页导航点击失败: %v", err)
		}
	}
	safeScreenshot(a.page, "./screenshot_comment_page_error.png")
	return fmt.Errorf("未能打开评论管理页，最后尝试 URL: %s，最后诊断: %s", lastURL, lastReason)
}

func detectCommentManagePage(page *rod.Page) (bool, string) {
	info, err := getCommentManageSurface(page)
	if err != nil {
		return false, fmt.Sprintf("页面检测失败: %v", err)
	}
	if info.OK {
		return true, info.Reason
	}
	return false, info.Reason
}

func waitForCommentManageSurface(page *rod.Page, timeout time.Duration) (bool, string) {
	deadline := time.Now().Add(timeout)
	var lastReason string
	for time.Now().Before(deadline) {
		ok, reason := detectCommentManagePage(page)
		if ok {
			return true, reason
		}
		lastReason = reason
		time.Sleep(800 * time.Millisecond)
	}
	return false, lastReason
}

type commentManageSurfaceInfo struct {
	OK              bool     `json:"ok"`
	Status          string   `json:"status"`
	Reason          string   `json:"reason"`
	URL             string   `json:"url"`
	BodyTextLen     int      `json:"bodyTextLen"`
	NodeCount       int      `json:"nodeCount"`
	ActionCount     int      `json:"actionCount"`
	EmptyState      bool     `json:"emptyState"`
	ResourceSamples []string `json:"resourceSamples"`
}

func getCommentManageSurface(page *rod.Page) (*commentManageSurfaceInfo, error) {
	result, err := page.Eval(`() => {
		const url = window.location.href;
		const text = (document.body && document.body.innerText || '').replace(/\s+/g, ' ').trim();
		const visible = (el) => {
			if (!el) return false;
			const style = window.getComputedStyle(el);
			const rect = el.getBoundingClientRect();
			return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
		};
		const attrText = (el) => {
			let out = '';
			for (const name of ['data-comment-id','comment-id','data-commentid','data-cid','cid','data-row-key','data-e2e','id','class','href']) {
				out += ' ' + (el.getAttribute && el.getAttribute(name) || '');
			}
			return out;
		};
		const clean = (s) => String(s || '').replace(/\s+/g, ' ').trim();
		const navHits = ['主页','创作','文章','视频','微头条','管理','作品管理','评论管理','草稿箱','数据','收益数据','粉丝数据','成长指南','工具','设置','关于今日头条','用户协议','隐私政策']
			.filter(s => text.includes(s)).length;
		const shellText = navHits >= 7 || text.includes('© 2026 toutiao.com') || text.includes('All Rights Reserved');
		const selectors = [
			'[data-comment-id]','[comment-id]','[data-cid]','[data-row-key]',
			'[class*="comment" i]','[data-e2e*="comment" i]','[class*="reply" i]',
			'.byte-table-row','.semi-table-row','tbody tr','li'
		].join(',');
		const shellAttrs = /pgc-wrapper|pgc-index|comment_manage-wrapper|shead|sidebar|nav|footer|menu/i;
		const nodes = Array.from(document.querySelectorAll(selectors)).filter(visible).filter(el => {
			const nodeText = clean(el.innerText || el.textContent);
			if (!nodeText || nodeText.length < 2) return false;
			if (shellAttrs.test(attrText(el))) return false;
			const hits = ['主页','创作','文章','视频','微头条','管理','作品管理','评论管理','草稿箱','数据','收益数据','粉丝数据','成长指南','工具','设置'].filter(s => nodeText.includes(s)).length;
			return hits < 6;
		});
		const actionCount = nodes.filter(el => {
			const t = clean(el.innerText || el.textContent).replace(/\s+/g, '');
			return t.includes('回复') || t.includes('赞') || t.includes('置顶') || t.includes('不置顶') || t.includes('删除') || t.includes('屏蔽') || t.includes('查看原文') || t.includes('查看对话');
		}).length;
		const emptyState = /暂无评论|暂无数据|没有评论|还没有评论|当前没有评论/.test(text);
		const resources = (performance.getEntriesByType && performance.getEntriesByType('resource') || [])
			.map(e => e.name || '')
			.filter(name => /comment|reply|interaction|message|notice|ugc|agw|api/i.test(name))
			.slice(-20);
		if (url.includes('/login') || text.includes('登录')) {
			return JSON.stringify({ok:false, status:'login', reason:'跳转到登录页，Cookie 可能已失效', url, bodyTextLen:text.length, nodeCount:nodes.length, actionCount, emptyState, resourceSamples:resources});
		}
		if (nodes.length > 0 && (actionCount > 0 || text.includes('全部评论') || text.includes('作品评论') || text.includes('用户评论'))) {
			return JSON.stringify({ok:true, status:'ready', reason:'url=' + url + ' nodes=' + nodes.length + ' actions=' + actionCount + ' body=' + text.length, url, bodyTextLen:text.length, nodeCount:nodes.length, actionCount, emptyState, resourceSamples:resources});
		}
		if (emptyState && !shellText) {
			return JSON.stringify({ok:true, status:'empty', reason:'url=' + url + ' empty_state body=' + text.length, url, bodyTextLen:text.length, nodeCount:nodes.length, actionCount, emptyState, resourceSamples:resources});
		}
		const status = shellText || text.length < 1800 ? 'shell' : 'loading';
		return JSON.stringify({ok:false, status, reason:'status=' + status + ' url=' + url + ' body=' + text.length + ' nodes=' + nodes.length + ' actions=' + actionCount + ' resources=' + resources.slice(0, 5).join('|') + ' sample=' + text.slice(0, 160), url, bodyTextLen:text.length, nodeCount:nodes.length, actionCount, emptyState, resourceSamples:resources});
	}`)
	if err != nil || result == nil {
		return nil, err
	}
	var info commentManageSurfaceInfo
	if err := json.Unmarshal([]byte(result.Value.Str()), &info); err != nil {
		return nil, fmt.Errorf("解析评论页状态失败: %s", result.Value.Str())
	}
	return &info, nil
}

func clickCommentManageNavigation(page *rod.Page) (string, error) {
	result, err := page.Eval(`() => {
		document.querySelectorAll('.mcp-comment-manage-nav').forEach(el => el.classList.remove('mcp-comment-manage-nav'));
		const visible = (el) => {
			if (!el) return false;
			const style = window.getComputedStyle(el);
			const rect = el.getBoundingClientRect();
			return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
		};
		const clean = (s) => String(s || '').replace(/\s+/g, '').trim();
		const textCandidates = [];
		const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
		let node;
		while ((node = walker.nextNode())) {
			const raw = node.nodeValue || '';
			const idx = raw.indexOf('评论管理');
			if (idx < 0) continue;
			const parent = node.parentElement;
			if (!visible(parent)) continue;
			const range = document.createRange();
			range.setStart(node, idx);
			range.setEnd(node, idx + '评论管理'.length);
			const rects = Array.from(range.getClientRects()).filter(r => r.width > 0 && r.height > 0);
			range.detach();
			for (const rect of rects) {
				if (rect.left > 260 || rect.top < 80) continue;
				const x = rect.left + rect.width / 2;
				const y = rect.top + rect.height / 2;
				const elAtPoint = document.elementFromPoint(x, y);
				let target = (elAtPoint && elAtPoint.closest('a, button, [role="button"], li, [class*="menu" i], [class*="nav" i], [class*="item" i]')) || parent;
				if (!visible(target)) target = parent;
				textCandidates.push({target, text: clean(target.innerText || target.textContent || parent.textContent), x, y, area: rect.width * rect.height});
			}
		}
		textCandidates.sort((a, b) => {
			const aExact = a.text === '评论管理' ? 0 : 1;
			const bExact = b.text === '评论管理' ? 0 : 1;
			if (aExact !== bExact) return aExact - bExact;
			return Math.abs(a.y - 396) - Math.abs(b.y - 396);
		});
		if (textCandidates.length) {
			const item = textCandidates[0];
			item.target.scrollIntoView({ block: 'center', inline: 'center' });
			item.target.classList.add('mcp-comment-manage-nav');
			return JSON.stringify({ok:true, detail:'range-click ' + item.text, x:item.x, y:item.y});
		}

		const candidates = Array.from(document.querySelectorAll('a, button, [role="button"], li, span, div'))
			.filter(visible)
			.map(el => {
				const rect = el.getBoundingClientRect();
				return {el, text: clean(el.innerText || el.textContent || el.getAttribute('aria-label') || el.getAttribute('title')), rect};
			})
			.filter(x => x.text === '评论管理' || x.text === '管理评论');
		for (const item of candidates) {
			if (!item.text || item.text.length > 6) continue;
			let target = item.el.closest('a, button, [role="button"], li, [class*="menu" i], [class*="nav" i], [class*="item" i]') || item.el;
			if (!visible(target)) target = item.el;
			target.scrollIntoView({ block: 'center', inline: 'center' });
			target.classList.add('mcp-comment-manage-nav');
			const rect = target.getBoundingClientRect();
			return JSON.stringify({ok:true, detail:'marked ' + item.text, x:rect.left + rect.width / 2, y:rect.top + rect.height / 2});
		}
		return JSON.stringify({ok:false, detail:'no visible comment manage nav, candidates=' + candidates.map(x => x.text).slice(0, 8).join('|')});
	}`)
	if err != nil {
		return "", err
	}
	if result == nil || result.Value.Str() == "" {
		return "", nil
	}
	var info struct {
		OK     bool    `json:"ok"`
		Detail string  `json:"detail"`
		X      float64 `json:"x"`
		Y      float64 `json:"y"`
	}
	_ = json.Unmarshal([]byte(result.Value.Str()), &info)
	if !info.OK {
		log.Warnf("未通过 DOM 精确定位评论管理导航，尝试截图经验坐标点击: %s", info.Detail)
		return "clicked fallback coordinate (133,396)", clickPagePoint(page, 133, 396)
	}
	if info.X > 0 && info.Y > 0 {
		if err := clickPagePoint(page, info.X, info.Y); err != nil {
			return info.Detail, err
		}
		return fmt.Sprintf("clicked %s at (%.1f, %.1f)", info.Detail, info.X, info.Y), nil
	}
	if err := physicalClickMarkedElement(page, ".mcp-comment-manage-nav"); err != nil {
		return info.Detail, err
	}
	return "clicked " + info.Detail, nil
}

func clickPagePoint(page *rod.Page, x, y float64) error {
	pt := proto.Point{X: x, Y: y}
	if err := page.Mouse.MoveTo(pt); err != nil {
		return err
	}
	if err := page.Mouse.Down(proto.InputMouseButtonLeft, 1); err != nil {
		return err
	}
	return page.Mouse.Up(proto.InputMouseButtonLeft, 1)
}

func activateCommentV2Flag(page *rod.Page) error {
	_, err := page.Eval(`() => {
		try {
			localStorage.setItem('comment-v2', 'true');
			sessionStorage.setItem('comment-v2', 'true');
			window.dispatchEvent(new StorageEvent('storage', {key: 'comment-v2', newValue: 'true'}));
			return true;
		} catch (err) {
			return false;
		}
	}`)
	return err
}

func prepareCommentPage(page *rod.Page) {
	if page == nil {
		return
	}
	_, _ = page.Activate()
	_ = page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width:             1440,
		Height:            1000,
		DeviceScaleFactor: 1,
		Mobile:            false,
	})
}

func loadCommentTargetIntoDOM(page *rod.Page, commentText string, timeout time.Duration) error {
	commentText = strings.TrimSpace(commentText)
	if commentText == "" {
		return nil
	}
	payload, _ := json.Marshal(commentText)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		result, err := page.Eval(`(payload) => {
			const targetText = JSON.parse(payload);
			const clean = (s) => String(s || '').replace(/\s+/g, ' ').trim();
			const visible = (el) => {
				if (!el) return false;
				const style = window.getComputedStyle(el);
				const rect = el.getBoundingClientRect();
				return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
			};
			const cards = Array.from(document.querySelectorAll('.all-comment-item-wrap .comment-item, .comment-item'))
				.filter(visible);
			if (cards.some(el => clean(el.innerText || el.textContent).includes(targetText))) {
				return JSON.stringify({found:true, cards:cards.length});
			}

			const loadMore = Array.from(document.querySelectorAll('button, a, [role="button"], span, div'))
				.filter(visible)
				.find(el => {
					const text = clean(el.textContent);
					return text === '加载更多' || text === '查看更多' || text === '更多评论';
				});
			if (loadMore) loadMore.click();

			const scrollers = Array.from(document.querySelectorAll(
				'.all-comment-wrap, .common-load-more-wrap, [class*="comment"][class*="scroll"], [class*="scroll"], main'
			)).filter(visible);
			for (const el of scrollers) {
				if (el.scrollHeight > el.clientHeight) el.scrollTop = el.scrollHeight;
			}
			window.scrollTo(0, document.body.scrollHeight);
			window.dispatchEvent(new Event('scroll', {bubbles:true}));
			return JSON.stringify({found:false, cards:cards.length, clickedLoadMore:Boolean(loadMore)});
		}`, string(payload))
		if err != nil {
			return err
		}
		if result != nil && strings.Contains(result.Value.Str(), `"found":true`) {
			return nil
		}
		time.Sleep(750 * time.Millisecond)
	}
	return fmt.Errorf("等待目标评论进入 DOM 超时")
}

func verifyCommentReplySubmitted(page *rod.Page, commentID, commentText, replyContent string, beforeReplyCount int, timeout time.Duration) (string, int, error) {
	payload, _ := json.Marshal(map[string]string{
		"commentText":  strings.TrimSpace(commentText),
		"replyContent": strings.TrimSpace(replyContent),
	})
	deadline := time.Now().Add(timeout)
	lastState := ""
	lastReplyCount := beforeReplyCount

	for time.Now().Before(deadline) {
		result, evalErr := page.Eval(`(payload) => {
			const opts = JSON.parse(payload);
			const clean = (s) => String(s || '').replace(/\s+/g, ' ').trim();
			const visible = (el) => {
				if (!el) return false;
				const style = window.getComputedStyle(el);
				const rect = el.getBoundingClientRect();
				return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
			};
			const visibleNodes = Array.from(document.querySelectorAll(
				'.byte-message, .semi-toast, [class*="toast" i], [class*="message" i], [role="alert"]'
			)).filter(visible);
			const notices = visibleNodes.map(el => clean(el.innerText || el.textContent)).filter(Boolean);
			const failure = notices.find(text => /回复失败|发送失败|提交失败|网络错误|操作失败|频繁|稍后重试/.test(text)) || '';
			const success = notices.find(text => /回复成功|发送成功|提交成功/.test(text)) || '';
			const cards = Array.from(document.querySelectorAll('.all-comment-item-wrap .comment-item, .comment-item'))
				.filter(visible)
				.filter(el => !opts.commentText || clean(el.innerText || el.textContent).includes(opts.commentText));
			const replyVisible = cards.some(card => {
				const replies = Array.from(card.querySelectorAll(
					'.sub-comment-item, .dongtai-list, [class*="reply-content" i], [class*="sub-comment" i]'
				));
				return replies.some(el => clean(el.innerText || el.textContent).includes(opts.replyContent));
			});
			const editors = Array.from(document.querySelectorAll(
				'.reply-box textarea, .comment-item-reply-box-wrap textarea, textarea, [contenteditable="true"]'
			)).filter(visible);
			const editorStillContains = editors.some(el => {
				const value = el.isContentEditable ? el.textContent : el.value;
				return clean(value).includes(opts.replyContent);
			});
			return JSON.stringify({
				failure,
				success,
				replyVisible,
				editorStillContains,
				cardCount: cards.length,
				editorCount: editors.length
			});
		}`, string(payload))
		if evalErr == nil && result != nil {
			lastState = result.Value.Str()
			var state struct {
				Failure             string `json:"failure"`
				Success             string `json:"success"`
				ReplyVisible        bool   `json:"replyVisible"`
				EditorStillContains bool   `json:"editorStillContains"`
				CardCount           int    `json:"cardCount"`
				EditorCount         int    `json:"editorCount"`
			}
			if json.Unmarshal([]byte(lastState), &state) == nil {
				if state.Failure != "" {
					return "", lastReplyCount, fmt.Errorf("评论回复提交失败，页面提示: %s", state.Failure)
				}
				if state.ReplyVisible && (commentID == "" || beforeReplyCount < 0) {
					return "回复内容已出现在目标评论下", lastReplyCount, nil
				}
			}
		}

		if commentID != "" && beforeReplyCount >= 0 {
			apiItem, apiErr := findCommentAPIItemByID(page, commentID)
			if apiErr == nil && apiItem != nil {
				lastReplyCount = apiItem.ReplyCount
				if apiItem.ReplyCount > beforeReplyCount {
					return fmt.Sprintf("真实接口 reply_count %d -> %d", beforeReplyCount, apiItem.ReplyCount), apiItem.ReplyCount, nil
				}
			}
		}
		time.Sleep(1200 * time.Millisecond)
	}

	return "", lastReplyCount, fmt.Errorf(
		"评论回复提交后验证失败：未检测到回复内容，reply_count=%d->%d，页面状态=%s",
		beforeReplyCount,
		lastReplyCount,
		truncateStr(lastState, 500),
	)
}

type commentAPIResponse struct {
	Code       int              `json:"code"`
	Message    string           `json:"message"`
	Data       []commentAPIItem `json:"data"`
	HasMore    bool             `json:"has_more"`
	NextOffset int              `json:"next_offset"`
}

type commentAPIItem struct {
	ID         int64  `json:"id"`
	IDStr      string `json:"id_str"`
	Text       string `json:"text"`
	CreateTime int64  `json:"create_time"`
	ReplyCount int    `json:"reply_count"`
	User       struct {
		Name string `json:"name"`
	} `json:"user"`
	ArticleInfo struct {
		GroupID    int64  `json:"group_id"`
		GroupIDStr string `json:"group_id_str"`
		Title      string `json:"title"`
	} `json:"article_info"`
}

type commentAPIFetchDiagnostic struct {
	Source       string `json:"source"`
	Pages        int    `json:"pages"`
	Fetched      int    `json:"fetched"`
	Matched      int    `json:"matched"`
	HasMore      bool   `json:"has_more"`
	NextOffset   int    `json:"next_offset"`
	ArticleID    string `json:"article_id,omitempty"`
	Keyword      string `json:"keyword,omitempty"`
	Requested    int    `json:"requested"`
	FallbackUsed bool   `json:"fallback_used"`
}

func fetchCommentsFromAPI(page *rod.Page, articleID, keyword string, limit int) ([]CommentItem, string, error) {
	if limit <= 0 {
		limit = 20
	}
	articleID = strings.TrimSpace(articleID)
	keyword = strings.TrimSpace(keyword)
	comments := make([]CommentItem, 0, limit)
	seen := make(map[string]struct{}, limit)
	offset := 0
	fetched := 0
	pages := 0
	hasMore := false

	for pages < 20 {
		resp, err := fetchCommentAPIBatch(page, offset, 50)
		if err != nil {
			return nil, "", err
		}
		pages++
		fetched += len(resp.Data)
		hasMore = resp.HasMore

		for _, apiItem := range resp.Data {
			item := commentItemFromAPI(apiItem)
			if item.CommentID == "" || item.Content == "" {
				continue
			}
			if articleID != "" && item.ArticleID != articleID {
				continue
			}
			if keyword != "" &&
				!strings.Contains(item.Content, keyword) &&
				!strings.Contains(item.UserName, keyword) &&
				!strings.Contains(item.RawText, keyword) {
				continue
			}
			if _, exists := seen[item.CommentID]; exists {
				continue
			}
			seen[item.CommentID] = struct{}{}
			comments = append(comments, item)
			if len(comments) >= limit {
				break
			}
		}

		if len(comments) >= limit || !resp.HasMore || resp.NextOffset <= offset || len(resp.Data) == 0 {
			offset = resp.NextOffset
			break
		}
		offset = resp.NextOffset
	}

	diagnostic, _ := json.Marshal(commentAPIFetchDiagnostic{
		Source:     "author_receive_comment_api",
		Pages:      pages,
		Fetched:    fetched,
		Matched:    len(comments),
		HasMore:    hasMore,
		NextOffset: offset,
		ArticleID:  articleID,
		Keyword:    keyword,
		Requested:  limit,
	})
	return comments, string(diagnostic), nil
}

func findCommentByIDFromAPI(page *rod.Page, commentID string) (*CommentItem, error) {
	apiItem, err := findCommentAPIItemByID(page, commentID)
	if err != nil {
		return nil, err
	}
	item := commentItemFromAPI(*apiItem)
	return &item, nil
}

func findCommentAPIItemByID(page *rod.Page, commentID string) (*commentAPIItem, error) {
	commentID = strings.TrimSpace(commentID)
	if commentID == "" {
		return nil, fmt.Errorf("comment_id 不能为空")
	}
	offset := 0
	for pageIndex := 0; pageIndex < 20; pageIndex++ {
		resp, err := fetchCommentAPIBatch(page, offset, 50)
		if err != nil {
			return nil, err
		}
		for _, apiItem := range resp.Data {
			if commentItemFromAPI(apiItem).CommentID == commentID {
				return &apiItem, nil
			}
		}
		if !resp.HasMore || resp.NextOffset <= offset || len(resp.Data) == 0 {
			break
		}
		offset = resp.NextOffset
	}
	return nil, fmt.Errorf("真实评论接口中未找到 comment_id=%s", commentID)
}

func fetchCommentAPIBatch(page *rod.Page, offset, count int) (*commentAPIResponse, error) {
	apiURL := fmt.Sprintf(
		"https://mp.toutiao.com/comment/author_receive_comment/?offset=%d&count=%d&scene=11&feature=0&user_relation=0&app_id=1231&_t=%d",
		offset,
		count,
		time.Now().UnixMilli(),
	)
	result, err := page.Timeout(15*time.Second).Eval(`async (url) => {
		try {
			const response = await fetch(url, {
				method: 'GET',
				credentials: 'include',
				cache: 'no-store',
				headers: {'Accept': 'application/json', 'Cache-Control': 'no-cache'}
			});
			const body = await response.text();
			return JSON.stringify({ok: response.ok, status: response.status, body});
		} catch (error) {
			return JSON.stringify({ok: false, status: 0, body: '', error: String(error)});
		}
	}`, apiURL)
	if err != nil {
		return nil, fmt.Errorf("调用评论真实接口失败: %w", err)
	}
	if result == nil || result.Value.Str() == "" {
		return nil, fmt.Errorf("评论真实接口未返回内容")
	}
	var envelope struct {
		OK     bool   `json:"ok"`
		Status int    `json:"status"`
		Body   string `json:"body"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal([]byte(result.Value.Str()), &envelope); err != nil {
		return nil, fmt.Errorf("解析评论接口 HTTP 响应失败: %w", err)
	}
	if !envelope.OK {
		return nil, fmt.Errorf("评论真实接口 HTTP %d: %s", envelope.Status, envelope.Error)
	}
	var resp commentAPIResponse
	if err := json.Unmarshal([]byte(envelope.Body), &resp); err != nil {
		return nil, fmt.Errorf("解析评论真实接口数据失败: %w", err)
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("评论真实接口返回错误 code=%d message=%s", resp.Code, resp.Message)
	}
	return &resp, nil
}

func commentItemFromAPI(apiItem commentAPIItem) CommentItem {
	commentID := strings.TrimSpace(apiItem.IDStr)
	if commentID == "" && apiItem.ID > 0 {
		commentID = fmt.Sprintf("%d", apiItem.ID)
	}
	articleID := strings.TrimSpace(apiItem.ArticleInfo.GroupIDStr)
	if articleID == "" && apiItem.ArticleInfo.GroupID > 0 {
		articleID = fmt.Sprintf("%d", apiItem.ArticleInfo.GroupID)
	}
	createTime := ""
	if apiItem.CreateTime > 0 {
		createTime = time.Unix(apiItem.CreateTime, 0).In(time.Local).Format("2006-01-02 15:04:05")
	}
	text := strings.TrimSpace(apiItem.Text)
	rawText := text
	if title := strings.TrimSpace(apiItem.ArticleInfo.Title); title != "" {
		rawText = title + " | " + text
	}
	return CommentItem{
		CommentID:  commentID,
		ArticleID:  articleID,
		UserName:   strings.TrimSpace(apiItem.User.Name),
		Content:    text,
		ReplyCount: apiItem.ReplyCount,
		CreateTime: createTime,
		RawText:    rawText,
	}
}

func scrapeCommentsOnCurrentPage(page *rod.Page, articleID, keyword string, limit int) ([]CommentItem, string, error) {
	if limit <= 0 {
		limit = 20
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"articleID": strings.TrimSpace(articleID),
		"keyword":   strings.TrimSpace(keyword),
		"limit":     limit,
	})
	var lastDiag string
	var lastErr error
	for attempt := 1; attempt <= 20; attempt++ {
		_, _ = page.Eval(`(attempt) => {
			const scrollers = Array.from(document.querySelectorAll('[class*="scroll" i], [class*="content" i], main, section, body')).filter(el => {
				const rect = el.getBoundingClientRect();
				return rect.width > 0 && rect.height > 0;
			});
			for (const el of scrollers.slice(0, 8)) {
				if (el.scrollHeight > el.clientHeight) {
					el.scrollTop = Math.min(el.scrollHeight, Math.max(0, attempt % 2 === 0 ? el.scrollHeight / 2 : 0));
				}
			}
			window.dispatchEvent(new Event('scroll', { bubbles: true }));
		}`, attempt)
		result, err := page.Eval(`(payload) => {
		const opts = JSON.parse(payload);
		const currentURL = window.location.href;
		const visible = (el) => {
			if (!el) return false;
			const style = window.getComputedStyle(el);
			const rect = el.getBoundingClientRect();
			return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
		};
		const clean = (s) => String(s || '').replace(/\s+/g, ' ').trim();
		const ownAttrText = (el) => {
			if (!el || !el.getAttribute) return '';
			const preferred = [
				'data-comment-id','data-comment_id','data-commentid','data-commentId',
				'comment-id','comment_id','commentid','commentId',
				'data-cid','data-comment-cid','cid',
				'data-reply-id','data-reply_id','data-replyid','data-replyId',
				'reply-id','reply_id','replyid','replyId',
				'data-id','data-key','data-row-key','data-item-id','data-group-id','data-log-id',
				'id','class','href','onclick','data-action','data-e2e'
			];
			const seen = new Set();
			const parts = [];
			const add = (name) => {
				if (!name || seen.has(name)) return;
				seen.add(name);
				const value = el.getAttribute(name);
				if (value != null && value !== '') parts.push(name + '=' + value);
			};
			preferred.forEach(add);
			// 头条后台经常调整 data-* 属性名；不能依赖一份静态白名单。
			for (const name of (el.getAttributeNames ? el.getAttributeNames() : [])) {
				if (/^(data-|aria-)|comment|reply|cid|(^|[-_:])id($|[-_:])|key|action|class|href|onclick/i.test(name)) add(name);
			}
			return parts.join(' ');
		};
		const attrText = (el) => {
			if (!el) return '';
			const parts = [ownAttrText(el)];
			// 候选选择器有时命中评论正文子节点，而 comment_id 挂在外层 React 卡片上。
			let parent = el.parentElement;
			for (let depth = 0; parent && depth < 5; depth++, parent = parent.parentElement) {
				parts.push(ownAttrText(parent));
				if (parent.matches && parent.matches('body, main, section, [class*="comment-list" i], [class*="table" i]')) break;
			}
			// 也兼容 ID 挂在卡片按钮、隐藏 input 或链接子节点上的情况。
			const children = el.querySelectorAll ? Array.from(el.querySelectorAll('*')).slice(0, 80) : [];
			for (const child of children) {
				const childAttrs = ownAttrText(child);
				if (/comment|reply|cid|data-id|row-key|item-id/i.test(childAttrs)) parts.push(childAttrs);
			}
			return parts.filter(Boolean).join(' ');
		};
		const findNumbers = (s) => {
			s = String(s || '');
			const out = [];
			for (const m of s.matchAll(/\d{5,22}/g)) {
				const value = m[0];
				const start = m.index || 0;
				const before = s.slice(Math.max(0, start - 12), start);
				const after = s.slice(start + value.length, start + value.length + 12);
				// 排除日期/时间、百分比、纯展示计数等正文数字；真实 comment_id 通常至少 10 位。
				if (value.length < 10) continue;
				if (/(\d{1,4}[-/.年]\d{1,2}[-/.月]?)$/.test(before) || /^[-/.月日:时分]/.test(after)) continue;
				if (/^[\s]*%/.test(after)) continue;
				out.push(value);
			}
			return out;
		};
		const reactIDs = (el) => {
			const found = [];
			const seen = new Set();
			const visit = (value, depth) => {
				if (!value || depth > 4 || found.length >= 8) return;
				if ((typeof value !== 'object' && typeof value !== 'function') || seen.has(value)) return;
				seen.add(value);
				let keys = [];
				try { keys = Object.keys(value).slice(0, 80); } catch (_) { return; }
				for (const key of keys) {
					let child;
					try { child = value[key]; } catch (_) { continue; }
					if (/^(comment[_-]?id|commentId|commentid|cid|reply[_-]?id|replyId)$/i.test(key)) {
						const match = String(child == null ? '' : child).match(/^\d{5,22}$/);
						if (match) found.push(match[0]);
					}
					if (depth < 4 && /props|memoizedProps|pendingProps|comment|reply|item|record|data|row/i.test(key)) visit(child, depth + 1);
				}
			};
			for (let node = el, depth = 0; node && depth < 5; node = node.parentElement, depth++) {
				let keys = [];
				try { keys = Object.keys(node); } catch (_) { continue; }
				for (const key of keys) {
					if (/^__react(?:Props|Fiber)\$/.test(key)) {
						try { visit(node[key], 0); } catch (_) {}
					}
				}
			}
			return found;
		};
		const extractID = (el, text) => {
			const attrs = attrText(el);
			const named = attrs.match(/(?:comment[_:-]?id|commentId|commentid|comment[_:-]?cid|data[_:-]?cid|(?:^|[\s"'/_:-])cid|reply[_:-]?id|replyId|replyid)[^0-9]{0,16}(\d{5,22})/i);
			if (named) return named[1];
			// class 常见形态：comment-item-1234567890 / reply_1234567890。
			const classNamed = attrs.match(/(?:comment|reply|cid)[a-z_-]{0,32}[-_:](\d{5,22})(?:\D|$)/i);
			if (classNamed) return classNamed[1];
			const fromReact = reactIDs(el).filter(n => n !== opts.articleID);
			if (fromReact.length) return fromReact[0];
			// 最后只从属性和很短的上下文中找长数字，避免把 06-25、300% 当成 ID。
			const safeText = String(text || '')
				.replace(/\b\d{1,4}[-/.]\d{1,2}(?:[-/.]\d{1,4})?(?:\s+\d{1,2}:\d{2})?\b/g, ' ')
				.replace(/\b\d+(?:\.\d+)?\s*%/g, ' ');
			const nums = findNumbers(attrs + ' ' + safeText).filter(n => n !== opts.articleID);
			return nums[0] || '';
		};
		const extractName = (el) => {
			const node = el.querySelector('.comment-item-title, .sub-comment-item-title, [class*="name" i], [class*="author" i], [class*="user" i], [class*="nick" i], [class*="avatar" i] + *');
			return clean(node && node.textContent).slice(0, 80);
		};
		const hasAction = (el) => {
			const buttons = Array.from(el.querySelectorAll('button, a, [role="button"], span, div')).filter(visible).map(x => (x.textContent || '').replace(/\s+/g, '').trim());
			return buttons.some(t => t === '回复' || t === '删除' || t === '置顶' || t === '屏蔽' || t === '举报' || t === '查看对话' || t === '查看原文' || t.startsWith('回复'));
		};
		const isLayoutShell = (el, text) => {
			const attrs = attrText(el);
			if (/pgc-wrapper|pgc-index|comment_manage-wrapper|shead|sidebar|nav|footer|menu/i.test(attrs)) return true;
			const navHits = ['主页','创作','文章','视频','微头条','管理','作品管理','评论管理','草稿箱','数据','收益数据','粉丝数据','成长指南','工具','设置','关于今日头条','用户协议','隐私政策'];
			const hitCount = navHits.filter(s => text.includes(s)).length;
			if (hitCount >= 7) return true;
			if (text.includes('© 2026 toutiao.com') || text.includes('All Rights Reserved')) return true;
			return false;
		};
		const badText = (text) => {
			if (!text) return true;
			if (text.length < 2 || text.length > 1800) return true;
			if (/^(主页|创作|文章|视频|微头条|管理|评论管理|数据|设置|帮助|搜索)$/.test(text)) return true;
			if (text.includes('登录') && text.includes('扫码')) return true;
			return false;
		};
		const selectors = [
			'.all-comment-item-wrap .comment-item', '.comment-item', '.sub-comment-item',
			'[data-comment-id]','[comment-id]','[data-cid]','[data-row-key]',
			'[class*="comment" i]','[data-e2e*="comment" i]','[class*="reply" i]',
			'[class*="message" i]','[class*="conversation" i]','[class*="feed" i] li',
			'.byte-table-row','.semi-table-row','tbody tr','li'
		].join(',');
		const nodes = Array.from(document.querySelectorAll(selectors))
			.filter(visible)
			.map(el => ({el, text: clean(el.innerText || el.textContent)}))
			.filter(x => !badText(x.text))
			.filter(x => !isLayoutShell(x.el, x.text))
			// articleID 只用于打开按文章筛选后的评论页。头条评论卡片通常不会把 group_id 渲染在卡片 DOM 中，
			// 因此这里不能再强制每个评论节点都包含 articleID，否则会把真实评论全部过滤掉。
			.filter(x => !opts.keyword || x.text.includes(opts.keyword));
		const scored = nodes.map(x => {
			let score = 0;
			const attrs = attrText(x.el);
			const id = extractID(x.el, x.text);
			const action = hasAction(x.el);
			if (/(data-comment-id|comment-id|data-commentid|commentid|data-cid|cid|reply_id|replyId)/i.test(attrs)) score += 5;
			if (/(comment[-_]?item|sub[-_]?comment[-_]?item|comment[-_]?card|reply[-_]?item|reply[-_]?card)/i.test(attrs)) score += 5;
			if (action) score += 5;
			if (x.text.includes('回复')) score += 2;
			if (x.text.includes('赞') || x.text.includes('删除') || x.text.includes('屏蔽')) score += 1;
			if (opts.keyword && x.text.includes(opts.keyword)) score += 8;
			if (id) score += 2;
			return {...x, score, id, action};
		}).filter(x => x.score > 0 && (x.action || x.id || opts.keyword)).sort((a, b) => b.score - a.score || a.text.length - b.text.length);
		const picked = [];
		const normalizedSeen = new Set();
		const takeTextKey = (s) => s.replace(/\s+/g, '').slice(0, 160);
		const contentFrom = (text) => {
			let t = text
				.replace(/^(全部评论|评论管理|互动管理|作品评论|用户评论)\s*/g, '')
				.replace(/\s*(回复|删除|举报|屏蔽|置顶|取消置顶|查看对话|查看原文|赞\s*\d*)\s*/g, ' ')
				.replace(/\s+/g, ' ')
				.trim();
			return t.slice(0, 500);
		};
		for (const x of scored) {
			if (picked.some(p => p.el === x.el || p.el.contains(x.el))) continue;
			if (picked.some(p => x.el.contains(p.el) && x.text.length > p.text.length * 1.5)) continue;
			const key = takeTextKey(contentFrom(x.text) || x.text);
			if (!key || isLayoutShell(x.el, key)) continue;
			if (normalizedSeen.has(key)) continue;
			normalizedSeen.add(key);
			picked.push(x);
			if (picked.length >= opts.limit) break;
		}
		const comments = picked.map(x => ({
			comment_id: extractID(x.el, x.text),
			article_id: opts.articleID || '',
			user_name: extractName(x.el),
			content: clean((x.el.querySelector('.comment-item-content, .sub-comment-item-content-wrap, [class*="comment-content" i]') || {}).textContent) || contentFrom(x.text),
			raw_text: x.text.slice(0, 1200)
		}));
		const bodyText = clean(document.body && document.body.innerText || '');
		const resourceSamples = (performance.getEntriesByType && performance.getEntriesByType('resource') || [])
			.map(e => e.name || '')
			.filter(name => /comment|reply|interaction|message|notice|ugc|agw|api/i.test(name))
			.slice(-25);
		return JSON.stringify({
			comments,
			diagnostic: {
				url: currentURL,
				bodyTextLen: bodyText.length,
				bodyTextSample: bodyText.slice(0, 300),
				nodeCount: nodes.length,
				scoredCount: scored.length,
				pickedCount: picked.length,
				samples: scored.slice(0, 8).map(x => ({score:x.score, text:x.text.slice(0, 180), attrs:attrText(x.el).slice(0, 160)})),
				idFailures: picked.filter(x => !extractID(x.el, x.text)).slice(0, 8).map(x => ({
					text: x.text.slice(0, 240),
					attrs: attrText(x.el).slice(0, 1200),
					html: String(x.el.outerHTML || '').slice(0, 1200)
				})),
				resourceSamples
			}
		});
	}`, string(payload))
		if err != nil {
			lastErr = err
			time.Sleep(800 * time.Millisecond)
			continue
		}
		var resp struct {
			Comments   []CommentItem `json:"comments"`
			Diagnostic struct {
				URL            string `json:"url"`
				BodyTextLen    int    `json:"bodyTextLen"`
				BodyTextSample string `json:"bodyTextSample"`
				NodeCount      int    `json:"nodeCount"`
				ScoredCount    int    `json:"scoredCount"`
				PickedCount    int    `json:"pickedCount"`
				Samples        []struct {
					Score int    `json:"score"`
					Text  string `json:"text"`
					Attrs string `json:"attrs"`
				} `json:"samples"`
				IDFailures []struct {
					Text  string `json:"text"`
					Attrs string `json:"attrs"`
					HTML  string `json:"html"`
				} `json:"idFailures"`
				ResourceSamples []string `json:"resourceSamples"`
			} `json:"diagnostic"`
		}
		if result == nil || result.Value.Str() == "" {
			lastErr = fmt.Errorf("评论页面未返回可解析内容")
			time.Sleep(800 * time.Millisecond)
			continue
		}
		if err := json.Unmarshal([]byte(result.Value.Str()), &resp); err != nil {
			raw := ""
			if result != nil {
				raw = result.Value.Str()
			}
			log.Warnf("解析评论列表失败，原始返回: %s", truncateStr(raw, 500))
			return nil, raw, fmt.Errorf("解析评论列表失败: %w", err)
		}
		beforeFilter := len(resp.Comments)
		resp.Comments = filterRealCommentItems(resp.Comments)
		if beforeFilter > 0 && len(resp.Comments) == 0 {
			log.Warnf("评论候选全部被 Go 侧壳节点过滤丢弃，原候选数=%d", beforeFilter)
		}
		diagBody, _ := json.Marshal(resp.Diagnostic)
		lastDiag = string(diagBody)
		log.Infof("评论列表抓取第 %d/20 次诊断: %s", attempt, lastDiag)
		if len(resp.Diagnostic.IDFailures) > 0 {
			log.Warnf("评论 ID 提取失败 %d 条（最多记录 8 条），DOM 诊断已包含 attrs/html 样本", len(resp.Diagnostic.IDFailures))
		}
		if len(resp.Comments) > 0 || strings.Contains(resp.Diagnostic.BodyTextSample, "暂无") || strings.Contains(resp.Diagnostic.BodyTextSample, "没有") {
			if len(resp.Comments) == 0 {
				log.Infof("评论列表抓取结果为空，页面诊断: %s", lastDiag)
			} else {
				log.Infof("评论列表抓取到 %d 条，页面诊断: %s", len(resp.Comments), lastDiag)
			}
			return resp.Comments, lastDiag, nil
		}
		time.Sleep(800 * time.Millisecond)
	}
	if lastErr != nil {
		return nil, lastDiag, lastErr
	}
	log.Warnf("评论列表等待后仍为空，最后页面诊断: %s", lastDiag)
	return []CommentItem{}, lastDiag, nil
}

func filterRealCommentItems(items []CommentItem) []CommentItem {
	filtered := make([]CommentItem, 0, len(items))
	for _, item := range items {
		if isCommentShellText(item.Content) || isCommentShellText(item.RawText) {
			continue
		}
		content := strings.TrimSpace(item.Content)
		raw := strings.TrimSpace(item.RawText)
		if content == "" && raw == "" {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func isCommentShellText(text string) bool {
	normalized := strings.TrimSpace(strings.Join(strings.Fields(text), " "))
	if normalized == "" {
		return true
	}
	navTerms := []string{"主页", "创作", "文章", "视频", "微头条", "管理", "作品管理", "评论管理", "草稿箱", "数据", "收益数据", "粉丝数据", "成长指南", "工具", "设置", "关于今日头条", "用户协议", "隐私政策"}
	hits := 0
	for _, term := range navTerms {
		if strings.Contains(normalized, term) {
			hits++
		}
	}
	if hits >= 7 {
		return true
	}
	if strings.Contains(normalized, "comment_manage-wrapper") ||
		strings.Contains(normalized, "pgc-wrapper") ||
		strings.Contains(normalized, "All Rights Reserved") ||
		strings.Contains(normalized, "© 2026 toutiao.com") {
		return true
	}
	return false
}

func markCommentReplyButton(page *rod.Page, articleID, commentID, commentText string) error {
	payload, _ := json.Marshal(map[string]string{
		"articleID":   articleID,
		"commentID":   commentID,
		"commentText": commentText,
	})
	result, err := page.Eval(`(payload) => {
		const opts = JSON.parse(payload);
		document.querySelectorAll('.mcp-comment-reply-button').forEach(el => el.classList.remove('mcp-comment-reply-button'));
		const visible = (el) => {
			if (!el) return false;
			const style = window.getComputedStyle(el);
			const rect = el.getBoundingClientRect();
			return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
		};
		const clean = (s) => String(s || '').replace(/\s+/g, '').trim();
		const loose = (s) => String(s || '').replace(/\s+/g, ' ').trim();
		const attrText = (el) => {
			let out = '';
			for (const name of ['data-comment-id','comment-id','data-id','data-item-id','data-group-id','id','class','href']) {
				out += ' ' + (el.getAttribute && el.getAttribute(name) || '');
			}
			return out;
		};
		const isLayoutShell = (el, text) => {
			const attrs = attrText(el);
			if (/pgc-wrapper|pgc-index|comment_manage-wrapper|shead|sidebar|nav|footer|menu/i.test(attrs)) return true;
			const navHits = ['主页','创作','文章','视频','微头条','管理','作品管理','评论管理','草稿箱','数据','收益数据','粉丝数据','成长指南','工具','设置','关于今日头条','用户协议','隐私政策'];
			const hitCount = navHits.filter(s => text.includes(s)).length;
			return hitCount >= 7 || text.includes('© 2026 toutiao.com') || text.includes('All Rights Reserved');
		};
		const blockSelectors = '.all-comment-item-wrap .comment-item, .comment-item, .sub-comment-item, [data-comment-id], [comment-id], [data-cid], [data-e2e*="comment" i]';
		const blocks = Array.from(document.querySelectorAll(blockSelectors)).filter(visible).filter(el => !isLayoutShell(el, loose(el.innerText || el.textContent)));
		const matches = (el) => {
			const text = loose(el.innerText || el.textContent);
			const attrs = attrText(el);
			// articleID 只用于打开按文章筛选后的评论页。评论卡片 DOM 通常不含 group_id，
			// 不能在这里继续按 articleID 过滤，否则会把所有评论块误删。
			if (opts.commentText && !text.includes(opts.commentText)) return false;
			// 真实 comment_id 往往只存在于接口数据或 React State。若 API 已补齐正文，
			// 正文是更可靠的 DOM 定位条件；仅在没有正文时才强制卡片包含 ID。
			if (opts.commentID && !opts.commentText && !text.includes(opts.commentID) && !attrs.includes(opts.commentID)) return false;
			return opts.commentID || opts.commentText;
		};
		const findReply = (root) => {
			const candidates = Array.from(root.querySelectorAll('button, a, span, div, [role="button"]')).filter(visible);
			for (const el of candidates) {
				const text = clean(el.textContent || el.getAttribute('aria-label') || el.getAttribute('title'));
				if (!text) continue;
				if ((text === '回复' || text === '立即回复' || text === '回作者' || text.startsWith('回复')) &&
					!text.includes('已回复') && !text.includes('回复数') && !text.includes('全部回复')) {
					const target = el.closest('button, a, [role="button"], [class*="button" i]') || el;
					target.scrollIntoView({ block: 'center', inline: 'center' });
					target.classList.add('mcp-comment-reply-button');
					return clean(target.textContent || text);
				}
			}
			return '';
		};
		for (const block of blocks) {
			if (!matches(block)) continue;
			const reply = findReply(block);
			if (reply) return JSON.stringify({ok:true, detail:'marked reply: ' + reply});
		}
		if (opts.commentText) {
			const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_ELEMENT);
			let node;
			while ((node = walker.nextNode())) {
				if (!visible(node)) continue;
				const text = loose(node.innerText || node.textContent);
				if (text.includes(opts.commentText)) {
					const reply = findReply(node.closest(blockSelectors) || node.parentElement || node);
					if (reply) return JSON.stringify({ok:true, detail:'marked reply by text: ' + reply});
				}
			}
		}
		return JSON.stringify({ok:false, detail:'no matching comment reply button'});
	}`, string(payload))
	if err != nil {
		return err
	}
	if result == nil {
		return fmt.Errorf("未找到评论回复按钮")
	}
	var info struct {
		OK     bool   `json:"ok"`
		Detail string `json:"detail"`
	}
	_ = json.Unmarshal([]byte(result.Value.Str()), &info)
	if !info.OK {
		return fmt.Errorf("未找到待回复评论或回复按钮: %s", info.Detail)
	}
	log.Infof("评论回复按钮定位成功: %s", info.Detail)
	return nil
}

func fillCommentReplyEditor(page *rod.Page, replyContent string) error {
	result, err := page.Eval(`() => {
		document.querySelectorAll('.mcp-comment-reply-editor').forEach(el => el.classList.remove('mcp-comment-reply-editor'));
		const visible = (el) => {
			if (!el) return false;
			const style = window.getComputedStyle(el);
			const rect = el.getBoundingClientRect();
			return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
		};
		const input = Array.from(document.querySelectorAll(
			'.reply-box textarea, .comment-item-reply-box-wrap textarea, textarea, [contenteditable="true"], [role="textbox"]'
		)).find(visible);
		if (input) {
			input.scrollIntoView({ block: 'center', inline: 'center' });
			input.classList.add('mcp-comment-reply-editor');
			return JSON.stringify({ok:true, detail:'marked visible reply editor'});
		}
		return JSON.stringify({ok:false, detail:'no reply editor'});
	}`)
	if err != nil {
		return err
	}
	var info struct {
		OK     bool   `json:"ok"`
		Detail string `json:"detail"`
	}
	if result != nil {
		_ = json.Unmarshal([]byte(result.Value.Str()), &info)
	}
	if !info.OK {
		return fmt.Errorf("未找到评论回复输入框: %s", info.Detail)
	}
	editor, err := page.Element(".mcp-comment-reply-editor")
	if err != nil {
		return fmt.Errorf("获取已标记的评论回复输入框失败: %w", err)
	}
	if editor == nil {
		return fmt.Errorf("获取已标记的评论回复输入框失败: element is nil")
	}
	if err := editor.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return fmt.Errorf("聚焦评论回复输入框失败: %w", err)
	}
	if err := editor.SelectAllText(); err != nil {
		return fmt.Errorf("选择评论回复输入框原内容失败: %w", err)
	}
	if err := editor.Input(replyContent); err != nil {
		return fmt.Errorf("物理输入评论回复内容失败: %w", err)
	}
	time.Sleep(350 * time.Millisecond)

	payload, _ := json.Marshal(replyContent)
	verify, err := page.Eval(`(payload) => {
		const expected = JSON.parse(payload);
		const input = document.querySelector('.mcp-comment-reply-editor');
		if (!input) return JSON.stringify({ok:false, detail:'marked editor disappeared'});
		const actual = input.isContentEditable ? input.textContent : input.value;
		const buttons = Array.from((input.closest('.reply-box, .comment-item-reply-box-wrap, [class*="reply-box" i]') || input.parentElement)
			.querySelectorAll('button, [role="button"]'));
		const publish = buttons.find(el => (el.textContent || '').replace(/\s+/g, '').trim() === '发布');
		const enabled = Boolean(publish) && !publish.disabled && publish.getAttribute('aria-disabled') !== 'true';
		const ok = String(actual || '').trim() === String(expected || '').trim() && enabled;
		return JSON.stringify({
			ok,
			detail:'value_match=' + (String(actual || '').trim() === String(expected || '').trim()) + ' publish_enabled=' + enabled,
			actual:String(actual || '').slice(0, 120)
		});
	}`, string(payload))
	if err != nil {
		return fmt.Errorf("校验评论回复输入状态失败: %w", err)
	}
	if verify == nil {
		return fmt.Errorf("评论回复输入状态无返回")
	}
	if err := json.Unmarshal([]byte(verify.Value.Str()), &info); err != nil {
		return fmt.Errorf("解析评论回复输入校验失败: %w", err)
	}
	if !info.OK {
		return fmt.Errorf("评论回复输入未固化或发布按钮未启用: %s", info.Detail)
	}
	log.Infof("评论回复输入已物理键入并校验: %s", info.Detail)
	return nil
}

func clickCommentReplySubmit(page *rod.Page) error {
	result, err := page.Eval(`() => {
		document.querySelectorAll('.mcp-comment-reply-submit').forEach(el => el.classList.remove('mcp-comment-reply-submit'));
		window.__mcpCommentReplySubmitClickObserved = false;
		const visible = (el) => {
			if (!el) return false;
			const style = window.getComputedStyle(el);
			const rect = el.getBoundingClientRect();
			return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
		};
		const clean = (s) => String(s || '').replace(/\s+/g, '').trim();
		const editors = Array.from(document.querySelectorAll(
			'.reply-box textarea, .comment-item-reply-box-wrap textarea, textarea, [contenteditable="true"], [role="textbox"]'
		)).filter(visible);
		const searchIn = editors.map(editor =>
			editor.closest('.reply-box, .comment-item-reply-box-wrap, [class*="reply-box" i], [role="dialog"]') ||
			editor.parentElement
		).filter(Boolean);
		for (const root of searchIn) {
			const candidates = Array.from(root.querySelectorAll('button, [role="button"], a')).filter(visible);
			for (const el of candidates) {
				const text = clean(el.textContent || el.getAttribute('aria-label') || el.getAttribute('title'));
				if ((text === '发布' || text === '发送' || text === '提交' || text === '确定' || text === '回复') &&
					!text.includes('取消') && !text.includes('全部回复') && !text.includes('已回复')) {
					const target = el.matches('button, a, [role="button"]') ? el : el.closest('button, a, [role="button"]');
					if (!target) continue;
					if (target.disabled || target.getAttribute('aria-disabled') === 'true') continue;
					target.scrollIntoView({ block: 'center', inline: 'center' });
					target.classList.add('mcp-comment-reply-submit');
					target.addEventListener('click', () => {
						window.__mcpCommentReplySubmitClickObserved = true;
					}, {capture:true, once:true});
					return JSON.stringify({ok:true, detail:'marked submit: ' + text});
				}
			}
		}
		return JSON.stringify({ok:false, detail:'no submit button in visible reply editor, editors=' + editors.length});
	}`)
	if err != nil {
		return err
	}
	var info struct {
		OK     bool   `json:"ok"`
		Detail string `json:"detail"`
	}
	if result != nil {
		_ = json.Unmarshal([]byte(result.Value.Str()), &info)
	}
	if !info.OK {
		return fmt.Errorf("未找到评论回复提交按钮: %s", info.Detail)
	}
	log.Infof("评论回复提交按钮定位成功: %s", info.Detail)
	button, err := page.Timeout(3 * time.Second).Element(".mcp-comment-reply-submit")
	if err != nil {
		return fmt.Errorf("获取评论回复提交按钮失败: %w", err)
	}
	if err := button.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return fmt.Errorf("物理点击评论回复提交按钮失败: %w", err)
	}
	time.Sleep(300 * time.Millisecond)
	clickObserved, err := page.Eval(`() => Boolean(window.__mcpCommentReplySubmitClickObserved)`)
	if err != nil {
		return fmt.Errorf("校验评论回复提交点击事件失败: %w", err)
	}
	if clickObserved == nil || !clickObserved.Value.Bool() {
		return fmt.Errorf("评论回复提交按钮未收到 click 事件")
	}
	log.Infof("评论回复提交按钮 click 事件已确认送达")
	return nil
}
