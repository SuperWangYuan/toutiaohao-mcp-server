package main

import (
	"net/http"

	"github.com/example/toutiaohao-mcp-server/toutiaohao"
	"github.com/gin-gonic/gin"
)

// handleHealth 健康检查
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

// apiLogin 登录 API
func (s *AppServer) apiLogin(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	result, err := s.toutiaoService.LoginWithCredentials(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	respondSuccess(c, result)
}

// apiCheckLoginStatus 检查登录状态 API
func (s *AppServer) apiCheckLoginStatus(c *gin.Context) {
	result, err := s.toutiaoService.CheckLoginStatus(c.Request.Context())
	if err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	respondSuccess(c, result)
}

// apiDeleteCookies 删除 Cookie API
func (s *AppServer) apiDeleteCookies(c *gin.Context) {
	if err := s.toutiaoService.DeleteCookies(c.Request.Context()); err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	respondSuccess(c, nil)
}

// apiPublishArticle 发布文章 API
func (s *AppServer) apiPublishArticle(c *gin.Context) {
	var req struct {
		Title      string   `json:"title"`
		Content    string   `json:"content"`
		Images     []string `json:"images"`
		Tags       []string `json:"tags"`
		Category   string   `json:"category"`
		CoverImage string   `json:"cover_image"`
		Original   bool     `json:"original"`
		Fiction    bool     `json:"fiction"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	opts := &toutiaohao.ArticleOptions{
		Images: req.Images, Tags: req.Tags, Category: req.Category,
		CoverImage: req.CoverImage, Original: req.Original, Fiction: req.Fiction,
	}
	if err := s.toutiaoService.PublishArticle(c.Request.Context(), req.Title, req.Content, opts); err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	respondSuccess(c, nil)
}

// apiPublishMicroPost 发布微头条 API
func (s *AppServer) apiPublishMicroPost(c *gin.Context) {
	var req struct {
		Content string   `json:"content"`
		Images  []string `json:"images"`
		Topic   string   `json:"topic"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.toutiaoService.PublishMicroPost(c.Request.Context(), req.Content, req.Images, req.Topic); err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	respondSuccess(c, nil)
}

// apiSaveMicroPostDraft 保存微头条草稿 API
func (s *AppServer) apiSaveMicroPostDraft(c *gin.Context) {
	var req struct {
		Content string   `json:"content"`
		Images  []string `json:"images"`
		Topic   string   `json:"topic"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.toutiaoService.SaveMicroPostDraft(c.Request.Context(), req.Content, req.Images, req.Topic); err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
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
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	respondSuccess(c, result)
}

// apiDeleteArticle 删除文章 API
func (s *AppServer) apiDeleteArticle(c *gin.Context) {
	var req struct {
		ArticleID string `json:"article_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.toutiaoService.DeleteArticle(c.Request.Context(), req.ArticleID); err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	respondSuccess(c, nil)
}

// apiGetAccountOverview 账户概览 API
func (s *AppServer) apiGetAccountOverview(c *gin.Context) {
	result, err := s.toutiaoService.GetAccountOverview(c.Request.Context())
	if err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	respondSuccess(c, result)
}

// apiGetArticleStats 文章统计 API
func (s *AppServer) apiGetArticleStats(c *gin.Context) {
	articleID := c.Query("article_id")
	result, err := s.toutiaoService.GetArticleStats(c.Request.Context(), articleID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	respondSuccess(c, result)
}

// apiGenerateReport 报告生成 API
func (s *AppServer) apiGenerateReport(c *gin.Context) {
	reportType := c.DefaultQuery("report_type", "weekly")
	result, err := s.toutiaoService.GenerateReport(c.Request.Context(), reportType)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	respondSuccess(c, result)
}
