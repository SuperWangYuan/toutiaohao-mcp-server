package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	log "github.com/sirupsen/logrus"
)

// AppServer 应用服务器，聚合所有组件
type AppServer struct {
	toutiaoService *ToutiaoService
	mcpServer      *mcp.Server
	router         *gin.Engine
	httpServer     *http.Server
}

// NewAppServer 创建 AppServer 实例
func NewAppServer(service *ToutiaoService) *AppServer {
	s := &AppServer{
		toutiaoService: service,
	}
	s.mcpServer = InitMCPServer(s)
	return s
}

// Start 启动 HTTP 服务
func (s *AppServer) Start(port string) error {
	gin.SetMode(gin.ReleaseMode)
	s.router = gin.New()
	s.router.Use(corsMiddleware())
	s.router.Use(errorHandlingMiddleware())
	s.router.Use(gin.Logger())

	setupRoutes(s.router, s)

	addr := fmt.Sprintf(":%s", port)
	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: s.router,
	}

	go func() {
		log.Infof("Server starting on %s", addr)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// 等待退出信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return s.httpServer.Shutdown(ctx)
}

// StartBackground 后台非阻塞启动 HTTP 服务（专门用于 Stdio 模式，端口占用不 Fatal 退出）
func (s *AppServer) StartBackground(port string) {
	gin.SetMode(gin.ReleaseMode)
	s.router = gin.New()
	s.router.Use(corsMiddleware())
	s.router.Use(errorHandlingMiddleware())
	s.router.Use(gin.Logger())

	setupRoutes(s.router, s)

	addr := fmt.Sprintf(":%s", port)
	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: s.router,
	}

	go func() {
		log.Infof("Secondary REST API server starting on %s (non-blocking for Stdio mode)", addr)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Warnf("Secondary REST API server failed (port %s may be occupied, REST API will be disabled but Stdio MCP remains active): %v", port, err)
		}
	}()
}
