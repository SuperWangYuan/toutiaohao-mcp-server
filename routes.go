package main

import (
	"github.com/gin-gonic/gin"
)

// setupRoutes 配置所有路由
func setupRoutes(router *gin.Engine, appServer *AppServer) {
	// MCP 端点
	mcpHandler := NewMCPHTTPHandler(appServer)
	router.Any("/mcp", gin.WrapH(mcpHandler))
	router.Any("/mcp/*path", gin.WrapH(mcpHandler))

	// 健康检查
	router.GET("/health", appServer.apiHealth)

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
		api.POST("/analytics/overview", appServer.apiGetAccountOverview) // 兼容 POST
		api.GET("/account/overview", appServer.apiGetAccountOverview)    // 兼容 /account/overview
		api.POST("/account/overview", appServer.apiGetAccountOverview)   // 兼容 /account/overview POST
		api.GET("/analytics/article", appServer.apiGetArticleStats)
		api.GET("/analytics/report", appServer.apiGenerateReport)
		api.GET("/analytics/trends", appServer.apiGetAccountTrends)
		api.GET("/articles/detail", appServer.apiGetArticleDetail)
		api.GET("/micro-posts", appServer.apiGetMicroPostList)
	}
}
