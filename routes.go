package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// setupRoutes 配置所有路由
func setupRoutes(router *gin.Engine, mcpServer *mcp.Server, appServer *AppServer) {
	// MCP 端点
	mcpHandler := NewMCPHTTPHandler(mcpServer)
	router.Any("/mcp", gin.WrapH(mcpHandler))
	router.Any("/mcp/*path", gin.WrapH(http.StripPrefix("/mcp", mcpHandler)))

	// 健康检查
	router.GET("/health", handleHealth)

	// REST API
	api := router.Group("/api/v1")
	{
		// 认证管理
		api.POST("/login", appServer.apiLogin)
		api.GET("/login/status", appServer.apiCheckLoginStatus)
		api.DELETE("/login/cookies", appServer.apiDeleteCookies)

		// 内容发布
		api.POST("/publish/article", appServer.apiPublishArticle)
		api.POST("/publish/micro", appServer.apiPublishMicroPost)
		api.POST("/publish/micro/draft", appServer.apiSaveMicroPostDraft)

		// 内容管理
		api.GET("/articles", appServer.apiGetArticleList)
		api.POST("/articles/delete", appServer.apiDeleteArticle)
		api.POST("/articles/update", appServer.apiUpdateArticle)

		// 数据分析
		api.GET("/analytics/overview", appServer.apiGetAccountOverview)
		api.GET("/analytics/article", appServer.apiGetArticleStats)
		api.GET("/analytics/report", appServer.apiGenerateReport)
	}
}
