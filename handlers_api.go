package main

import (
	"net/http"
	"strings"

	"github.com/example/toutiaohao-mcp-server/toutiaohao"
	"github.com/gin-gonic/gin"
)

// apiHealth 健康检查，返回极速自检登录态
func (s *AppServer) apiHealth(c *gin.Context) {
	status, err := s.toutiaoService.CheckLoginStatus(c.Request.Context())
	loggedIn := false
	loginMsg := "No cookies found"
	if err == nil && status != nil {
		loggedIn = status.LoggedIn
		loginMsg = status.Message
	}

	c.JSON(http.StatusOK, gin.H{
		"status":        "ok",
		"logged_in":     loggedIn,
		"login_message": loginMsg,
	})
}

func handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// respondSuccess 统一成功响应
func respondSuccess(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Message: "ok",
		Data:    data,
	})
}

// respondError 统一错误响应
func respondError(c *gin.Context, status int, message string) {
	c.JSON(status, ErrorResponse{
		Success: false,
		Message: message,
	})
}

// mapErrorToStatusCode 智能映射错误类型为 HTTP 状态码
func mapErrorToStatusCode(err error) int {
	if err == nil {
		return http.StatusOK
	}
	errMsg := err.Error()
	// 常见的参数校验、查重、登录态失效、参数限制等业务问题归类为 400 Bad Request
	if strings.Contains(errMsg, "已存在") ||
		strings.Contains(errMsg, "已过期") ||
		strings.Contains(errMsg, "失效") ||
		strings.Contains(errMsg, "登录") ||
		strings.Contains(errMsg, "校验") ||
		strings.Contains(errMsg, "验证") ||
		strings.Contains(errMsg, "invalid") ||
		strings.Contains(errMsg, "required") ||
		strings.Contains(errMsg, "limit") ||
		strings.Contains(errMsg, "missing") ||
		strings.Contains(errMsg, "too long") ||
		strings.Contains(errMsg, "格式") ||
		strings.Contains(errMsg, "为空") ||
		strings.Contains(errMsg, "冲突") {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

// apiLogin 登录 API
func (s *AppServer) apiLogin(c *gin.Context) {
	var req struct {
		Username string `json:"username" form:"username"`
		Password string `json:"password" form:"password"`
	}
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	result, err := s.toutiaoService.LoginWithCredentials(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		respondError(c, mapErrorToStatusCode(err), err.Error())
		return
	}
	respondSuccess(c, result)
}

// apiCheckLoginStatus 检查登录状态 API
func (s *AppServer) apiCheckLoginStatus(c *gin.Context) {
	result, err := s.toutiaoService.CheckLoginStatus(c.Request.Context())
	if err != nil {
		respondError(c, mapErrorToStatusCode(err), err.Error())
		return
	}
	respondSuccess(c, result)
}

// apiDeleteCookies 删除 Cookie API
func (s *AppServer) apiDeleteCookies(c *gin.Context) {
	if err := s.toutiaoService.DeleteCookies(c.Request.Context()); err != nil {
		respondError(c, mapErrorToStatusCode(err), err.Error())
		return
	}
	respondSuccess(c, nil)
}

// apiPublishArticle 发布文章 API
func (s *AppServer) apiPublishArticle(c *gin.Context) {
	var req struct {
		Title       string      `json:"title" form:"title"`
		Content     string      `json:"content" form:"content"`
		Images      []string    `json:"images" form:"images"`
		Tags        []string    `json:"tags" form:"tags"`
		Category    string      `json:"category" form:"category"`
		CoverImage  string      `json:"cover_image" form:"cover_image"`
		Original    bool        `json:"original" form:"original"`
		Fiction     bool        `json:"fiction" form:"fiction"`
		PublishTime interface{} `json:"publish_time" form:"publish_time"`
		SaveAsDraft bool        `json:"save_as_draft" form:"save_as_draft"`
	}
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	opts := &toutiaohao.ArticleOptions{
		Images: req.Images, Tags: req.Tags, Category: req.Category,
		CoverImage: req.CoverImage, Original: req.Original, Fiction: req.Fiction,
		PublishTime: req.PublishTime, SaveAsDraft: req.SaveAsDraft,
	}
	res, err := s.toutiaoService.PublishArticle(c.Request.Context(), req.Title, req.Content, opts)
	if err != nil {
		respondError(c, mapErrorToStatusCode(err), err.Error())
		return
	}
	respondSuccess(c, res)
}

// apiPublishMicroPost 发布微头条 API
func (s *AppServer) apiPublishMicroPost(c *gin.Context) {
	var req struct {
		Content     string      `json:"content" form:"content"`
		Images      []string    `json:"images" form:"images"`
		Topic       string      `json:"topic" form:"topic"`
		PublishTime interface{} `json:"publish_time" form:"publish_time"`
	}
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.toutiaoService.PublishMicroPost(c.Request.Context(), req.Content, req.Images, req.Topic, req.PublishTime); err != nil {
		respondError(c, mapErrorToStatusCode(err), err.Error())
		return
	}
	respondSuccess(c, nil)
}

// apiSaveMicroPostDraft 保存微头条草稿 API
func (s *AppServer) apiSaveMicroPostDraft(c *gin.Context) {
	var req struct {
		Content string   `json:"content" form:"content"`
		Images  []string `json:"images" form:"images"`
		Topic   string   `json:"topic" form:"topic"`
	}
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.toutiaoService.SaveMicroPostDraft(c.Request.Context(), req.Content, req.Images, req.Topic); err != nil {
		respondError(c, mapErrorToStatusCode(err), err.Error())
		return
	}
	respondSuccess(c, nil)
}

// apiGetArticleList 获取文章列表 API
func (s *AppServer) apiGetArticleList(c *gin.Context) {
	params := toutiaohao.NewArticleListParams(map[string]interface{}{
		"status": c.DefaultQuery("status", "all"),
	})

	result, err := s.toutiaoService.GetArticleList(c.Request.Context(), params)
	if err != nil {
		respondError(c, mapErrorToStatusCode(err), err.Error())
		return
	}
	respondSuccess(c, result)
}

// apiDeleteArticle 删除文章 API
func (s *AppServer) apiDeleteArticle(c *gin.Context) {
	var req struct {
		ArticleID string `json:"article_id" form:"article_id"`
	}
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.toutiaoService.DeleteArticle(c.Request.Context(), req.ArticleID); err != nil {
		respondError(c, mapErrorToStatusCode(err), err.Error())
		return
	}
	respondSuccess(c, nil)
}

// apiUpdateArticle 修改文章 API
func (s *AppServer) apiUpdateArticle(c *gin.Context) {
	var req struct {
		ArticleID   string      `json:"article_id" form:"article_id"`
		Title       string      `json:"title" form:"title"`
		Content     string      `json:"content" form:"content"`
		Images      []string    `json:"images" form:"images"`
		Tags        []string    `json:"tags" form:"tags"`
		Category    string      `json:"category" form:"category"`
		CoverImage  string      `json:"cover_image" form:"cover_image"`
		Original    bool        `json:"original" form:"original"`
		Fiction     bool        `json:"fiction" form:"fiction"`
		PublishTime interface{} `json:"publish_time" form:"publish_time"`
		SaveAsDraft bool        `json:"save_as_draft" form:"save_as_draft"`
	}
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	opts := &toutiaohao.ArticleOptions{
		Images: req.Images, Tags: req.Tags, Category: req.Category,
		CoverImage: req.CoverImage, Original: req.Original, Fiction: req.Fiction,
		PublishTime: req.PublishTime, SaveAsDraft: req.SaveAsDraft,
	}
	res, err := s.toutiaoService.UpdateArticle(c.Request.Context(), req.ArticleID, req.Title, req.Content, opts)
	if err != nil {
		respondError(c, mapErrorToStatusCode(err), err.Error())
		return
	}
	respondSuccess(c, res)
}

// apiGetAccountOverview 账户概览 API
func (s *AppServer) apiGetAccountOverview(c *gin.Context) {
	result, err := s.toutiaoService.GetAccountOverview(c.Request.Context())
	if err != nil {
		respondError(c, mapErrorToStatusCode(err), err.Error())
		return
	}
	respondSuccess(c, result)
}

// apiGetArticleStats 文章统计 API
func (s *AppServer) apiGetArticleStats(c *gin.Context) {
	articleID := c.Query("article_id")
	result, err := s.toutiaoService.GetArticleStats(c.Request.Context(), articleID)
	if err != nil {
		respondError(c, mapErrorToStatusCode(err), err.Error())
		return
	}
	respondSuccess(c, result)
}

// apiGenerateReport 报告生成 API
func (s *AppServer) apiGenerateReport(c *gin.Context) {
	reportType := c.DefaultQuery("report_type", "weekly")
	result, err := s.toutiaoService.GenerateReport(c.Request.Context(), reportType)
	if err != nil {
		respondError(c, mapErrorToStatusCode(err), err.Error())
		return
	}
	respondSuccess(c, result)
}
