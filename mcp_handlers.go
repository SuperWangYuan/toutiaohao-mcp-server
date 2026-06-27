package main

import (
	"context"
	"encoding/json"

	"github.com/example/toutiaohao-mcp-server/configs"
	"github.com/example/toutiaohao-mcp-server/toutiaohao"
	log "github.com/sirupsen/logrus"
)

func truncateTitleForPublish(title string) string {
	runes := []rune(title)
	if len(runes) <= configs.MaxTitleLength {
		return title
	}
	truncated := string(runes[:configs.MaxTitleLength])
	log.Warnf("标题超过 %d 字，已在入口层自动截断: %s -> %s", configs.MaxTitleLength, title, truncated)
	return truncated
}

// handleLoginArgsValidation 校验登录参数，返回 nil 表示通过
func handleLoginArgsValidation(args map[string]interface{}) *MCPToolResult {
	username, _ := args["username"].(string)
	password, _ := args["password"].(string)

	if err := toutiaohao.ValidateLoginRequest(username, password); err != nil {
		return NewErrorResult(err.Error())
	}
	return nil
}

// handleLogin 处理登录请求
func (s *AppServer) handleLogin(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	if errResult := handleLoginArgsValidation(args); errResult != nil {
		return errResult
	}

	username, _ := args["username"].(string)
	password, _ := args["password"].(string)

	result, err := s.toutiaoService.LoginWithCredentials(ctx, username, password)
	if err != nil {
		return NewErrorResult(err.Error())
	}

	data, _ := json.Marshal(result)
	return NewTextResult(string(data))
}

// handleCheckLoginStatus 处理登录状态检查
func (s *AppServer) handleCheckLoginStatus(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	result, err := s.toutiaoService.CheckLoginStatus(ctx)
	if err != nil {
		return NewErrorResult(err.Error())
	}

	data, _ := json.Marshal(result)
	return NewTextResult(string(data))
}

// handleDeleteCookies 处理删除 Cookie
func (s *AppServer) handleDeleteCookies(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	if err := s.toutiaoService.DeleteCookies(ctx); err != nil {
		return NewErrorResult(err.Error())
	}
	return NewTextResult(`{"success": true, "message": "Cookies deleted"}`)
}

// handlePublishMicroPost 处理微头条发布
func (s *AppServer) handlePublishMicroPost(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	content, _ := args["content"].(string)
	topic, _ := args["topic"].(string)
	publishTime := args["publish_time"]

	var images []string
	if imgs, ok := args["images"].([]string); ok {
		images = imgs
	}

	if err := toutiaohao.ValidateMicroPost(content, images, topic); err != nil {
		return NewErrorResult(err.Error())
	}

	if err := s.toutiaoService.PublishMicroPost(ctx, content, images, topic, publishTime); err != nil {
		return NewErrorResult(err.Error())
	}

	return NewTextResult(`{"success": true, "message": "Micro post published"}`)
}

// handleSaveMicroPostDraft 处理微头条草稿保存
func (s *AppServer) handleSaveMicroPostDraft(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	content, _ := args["content"].(string)
	topic, _ := args["topic"].(string)

	var images []string
	if imgs, ok := args["images"].([]string); ok {
		images = imgs
	}

	if err := toutiaohao.ValidateMicroPost(content, images, topic); err != nil {
		return NewErrorResult(err.Error())
	}

	if err := s.toutiaoService.SaveMicroPostDraft(ctx, content, images, topic); err != nil {
		return NewErrorResult(err.Error())
	}

	return NewTextResult(`{"success": true, "message": "Draft saved"}`)
}

// handlePublishArticle 处理文章发布
func (s *AppServer) handlePublishArticle(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	title, _ := args["title"].(string)
	content, _ := args["content"].(string)
	title = truncateTitleForPublish(title)

	opts := &toutiaohao.ArticleOptions{}
	if imgs, ok := args["images"].([]string); ok {
		opts.Images = imgs
	}
	if tags, ok := args["tags"].([]string); ok {
		opts.Tags = tags
	}
	if cat, ok := args["category"].(string); ok {
		opts.Category = cat
	}
	if cover, ok := args["cover_image"].(string); ok {
		opts.CoverImage = cover
	}
	if orig, ok := args["original"].(bool); ok {
		opts.Original = orig
	}
	if fiction, ok := args["fiction"].(bool); ok {
		opts.Fiction = fiction
	}
	if publishTime, ok := args["publish_time"]; ok {
		opts.PublishTime = publishTime
	}
	if saveDraft, ok := args["save_as_draft"].(bool); ok {
		opts.SaveAsDraft = saveDraft
	}

	if err := toutiaohao.ValidateArticle(title, content, opts); err != nil {
		return NewErrorResult(err.Error())
	}

	res, err := s.toutiaoService.PublishArticle(ctx, title, content, opts)
	if err != nil {
		return NewErrorResult(err.Error())
	}

	data, _ := json.Marshal(res)
	return NewTextResult(string(data))
}

// handleUpdateArticle 处理文章修改
func (s *AppServer) handleUpdateArticle(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	articleID, _ := args["article_id"].(string)
	title, _ := args["title"].(string)
	content, _ := args["content"].(string)
	if title != "" {
		title = truncateTitleForPublish(title)
	}

	opts := &toutiaohao.ArticleOptions{}
	if imgs, ok := args["images"].([]string); ok {
		opts.Images = imgs
	}
	if tags, ok := args["tags"].([]string); ok {
		opts.Tags = tags
	}
	if cat, ok := args["category"].(string); ok {
		opts.Category = cat
	}
	if cover, ok := args["cover_image"].(string); ok {
		opts.CoverImage = cover
	}
	if orig, ok := args["original"].(bool); ok {
		opts.Original = orig
	}
	if fiction, ok := args["fiction"].(bool); ok {
		opts.Fiction = fiction
	}
	if publishTime, ok := args["publish_time"]; ok {
		opts.PublishTime = publishTime
	}
	if saveDraft, ok := args["save_as_draft"].(bool); ok {
		opts.SaveAsDraft = saveDraft
	}

	res, err := s.toutiaoService.UpdateArticle(ctx, articleID, title, content, opts)
	if err != nil {
		return NewErrorResult(err.Error())
	}

	data, _ := json.Marshal(res)
	return NewTextResult(string(data))
}

// handleGetArticleList 处理文章列表查询
func (s *AppServer) handleGetArticleList(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	params := toutiaohao.NewArticleListParams(args)
	if err := toutiaohao.ValidateArticleListStatus(params.Status); err != nil {
		return NewErrorResult(err.Error())
	}

	result, err := s.toutiaoService.GetArticleList(ctx, params)
	if err != nil {
		return NewErrorResult(err.Error())
	}

	data, _ := json.Marshal(result)
	return NewTextResult(string(data))
}

// handleDeleteArticle 处理文章删除
func (s *AppServer) handleDeleteArticle(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	articleID, _ := args["article_id"].(string)
	if err := toutiaohao.ValidateDeleteArticle(articleID); err != nil {
		return NewErrorResult(err.Error())
	}

	if err := s.toutiaoService.DeleteArticle(ctx, articleID); err != nil {
		return NewErrorResult(err.Error())
	}

	return NewTextResult(`{"success": true, "message": "Article deleted"}`)
}

// handleGetComments 处理评论列表查询
func (s *AppServer) handleGetComments(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	params := toutiaohao.NewCommentListParams(args)
	if err := toutiaohao.ValidateGetComments(params); err != nil {
		return NewErrorResult(err.Error())
	}

	result, err := s.toutiaoService.GetComments(ctx, params)
	if err != nil {
		return NewErrorResult(err.Error())
	}

	data, _ := json.Marshal(result)
	return NewTextResult(string(data))
}

// handleProbeCommentManage 处理评论管理页诊断
func (s *AppServer) handleProbeCommentManage(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	params := &toutiaohao.CommentProbeParams{}
	if args != nil {
		params.ArticleID, _ = args["article_id"].(string)
		if wait, ok := args["wait_ms"].(int); ok {
			params.WaitMS = wait
		} else if wait, ok := args["wait_ms"].(float64); ok {
			params.WaitMS = int(wait)
		}
	}

	result, err := s.toutiaoService.ProbeCommentManage(ctx, params)
	if err != nil {
		return NewErrorResult(err.Error())
	}

	data, _ := json.Marshal(result)
	return NewTextResult(string(data))
}

// handleReplyComment 处理评论回复
func (s *AppServer) handleReplyComment(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	articleID, _ := args["article_id"].(string)
	commentID, _ := args["comment_id"].(string)
	commentText, _ := args["comment_text"].(string)
	replyContent, _ := args["reply_content"].(string)
	if err := toutiaohao.ValidateReplyComment(articleID, commentID, commentText, replyContent); err != nil {
		return NewErrorResult(err.Error())
	}

	result, err := s.toutiaoService.ReplyComment(ctx, articleID, commentID, commentText, replyContent)
	if err != nil {
		return NewErrorResult(err.Error())
	}

	data, _ := json.Marshal(result)
	return NewTextResult(string(data))
}

// handleGetAccountOverview 处理账户概览
func (s *AppServer) handleGetAccountOverview(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	result, err := s.toutiaoService.GetAccountOverview(ctx)
	if err != nil {
		return NewErrorResult(err.Error())
	}

	data, _ := json.Marshal(result)
	return NewTextResult(string(data))
}

// handleGetArticleStats 处理文章统计
func (s *AppServer) handleGetArticleStats(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	articleID, _ := args["article_id"].(string)
	if err := toutiaohao.ValidateArticleStats(articleID); err != nil {
		return NewErrorResult(err.Error())
	}

	result, err := s.toutiaoService.GetArticleStats(ctx, articleID)
	if err != nil {
		return NewErrorResult(err.Error())
	}

	data, _ := json.Marshal(result)
	return NewTextResult(string(data))
}

// handleGenerateReport 处理报告生成
func (s *AppServer) handleGenerateReport(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	reportType, _ := args["report_type"].(string)
	reportType = toutiaohao.NormalizeReportType(reportType)

	if err := toutiaohao.ValidateReportType(reportType); err != nil {
		return NewErrorResult(err.Error())
	}

	result, err := s.toutiaoService.GenerateReport(ctx, reportType)
	if err != nil {
		return NewErrorResult(err.Error())
	}

	data, _ := json.Marshal(result)
	return NewTextResult(string(data))
}

// handleGetArticleDetail 处理获取单篇文章详情
func (s *AppServer) handleGetArticleDetail(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	articleID, _ := args["article_id"].(string)
	if articleID == "" {
		return NewErrorResult("article_id is required")
	}

	result, err := s.toutiaoService.GetArticleDetail(ctx, articleID)
	if err != nil {
		return NewErrorResult(err.Error())
	}

	data, _ := json.Marshal(result)
	return NewTextResult(string(data))
}

// handleGetMicroPosts 处理获取微头条列表
func (s *AppServer) handleGetMicroPosts(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	params := toutiaohao.NewArticleListParams(args)
	params.ContentType = "ugc" // 微头条

	if err := toutiaohao.ValidateArticleListStatus(params.Status); err != nil {
		return NewErrorResult(err.Error())
	}

	result, err := s.toutiaoService.GetArticleList(ctx, params)
	if err != nil {
		return NewErrorResult(err.Error())
	}

	data, _ := json.Marshal(result)
	return NewTextResult(string(data))
}

// handleGetAccountTrends 处理获取账户趋势数据
func (s *AppServer) handleGetAccountTrends(ctx context.Context, args map[string]interface{}) *MCPToolResult {
	var days int
	if d, ok := args["days"].(float64); ok {
		days = int(d)
	} else if d, ok := args["days"].(int); ok {
		days = d
	}
	if days <= 0 {
		days = 7
	}

	result, err := s.toutiaoService.GetAccountTrends(ctx, days)
	if err != nil {
		return NewErrorResult(err.Error())
	}

	data, _ := json.Marshal(result)
	return NewTextResult(string(data))
}
