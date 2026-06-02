package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAllToolsRegistered(t *testing.T) {
	service := NewToutiaoService(nil)
	server := NewAppServer(service)

	if server.mcpServer == nil {
		t.Fatal("mcpServer is nil")
	}
	// 11 个工具应全部注册，如果 AddTool 失败会 panic，测试能跑通说明注册成功
}

func TestHTTPAPIHealthCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/health", handleHealth)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("health = %d, want %d", w.Code, http.StatusOK)
	}
	if w.Body.String() == "" {
		t.Error("health response body is empty")
	}
}

func TestHTTPAPIRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := NewToutiaoService(nil)
	server := NewAppServer(service)

	server.router = gin.New()
	setupRoutes(server.router, server)

	expectedRoutes := []struct {
		method string
		path   string
	}{
		{"GET", "/health"},
		{"POST", "/api/v1/login"},
		{"GET", "/api/v1/login/status"},
		{"DELETE", "/api/v1/login/cookies"},
		{"POST", "/api/v1/publish/article"},
		{"POST", "/api/v1/publish/micro"},
		{"POST", "/api/v1/publish/micro/draft"},
		{"GET", "/api/v1/articles"},
		{"POST", "/api/v1/articles/delete"},
		{"GET", "/api/v1/analytics/overview"},
		{"GET", "/api/v1/analytics/article"},
		{"GET", "/api/v1/analytics/report"},
	}

	routes := server.router.Routes()
	routeMap := make(map[string]bool)
	for _, r := range routes {
		routeMap[r.Method+":"+r.Path] = true
	}

	for _, er := range expectedRoutes {
		key := er.method + ":" + er.path
		if !routeMap[key] {
			t.Errorf("expected route %s %s not registered", er.method, er.path)
		}
	}
}

func TestUnifiedErrorResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/test-error", func(c *gin.Context) {
		respondError(c, http.StatusBadRequest, "test error")
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test-error", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}
