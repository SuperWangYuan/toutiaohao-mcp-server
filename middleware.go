package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// corsMiddleware CORS 中间件
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, Mcp-Session-Id, Mcp-Protocol-Version")
		c.Header("Access-Control-Expose-Headers", "Mcp-Session-Id, Mcp-Protocol-Version")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// errorHandlingMiddleware panic 恢复中间件
func errorHandlingMiddleware() gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(gin.DefaultWriter, func(c *gin.Context, err interface{}) {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Success: false,
			Message: "Internal server error",
		})
	})
}
