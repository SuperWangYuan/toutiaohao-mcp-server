package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestConvertToMCPResult_Text(t *testing.T) {
	result := NewTextResult("hello")
	mcpResult := convertToMCPResult(result)

	if mcpResult.IsError {
		t.Error("expected IsError=false")
	}
	if len(mcpResult.Content) != 1 {
		t.Fatalf("expected 1 content, got %d", len(mcpResult.Content))
	}
}

func TestConvertToMCPResult_Error(t *testing.T) {
	result := NewErrorResult("something failed")
	mcpResult := convertToMCPResult(result)

	if !mcpResult.IsError {
		t.Error("expected IsError=true")
	}
	if len(mcpResult.Content) != 1 {
		t.Fatalf("expected 1 content, got %d", len(mcpResult.Content))
	}
}

func TestConvertToMCPResult_Image(t *testing.T) {
	result := NewImageResult("image/png", "iVBOR...")
	mcpResult := convertToMCPResult(result)

	if mcpResult.IsError {
		t.Error("expected IsError=false")
	}
	if len(mcpResult.Content) != 1 {
		t.Fatalf("expected 1 content, got %d", len(mcpResult.Content))
	}
}

func TestConvertToMCPResult_Nil(t *testing.T) {
	mcpResult := convertToMCPResult(nil)

	if !mcpResult.IsError {
		t.Error("expected IsError=true for nil result")
	}
}

func TestWithPanicRecovery(t *testing.T) {
	panicHandler := func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
		panic("test panic")
	}

	wrapped := withPanicRecovery("test_tool", panicHandler)

	// Should not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("withPanicRecovery did not catch panic: %v", r)
		}
	}()

	wrapped(context.Background(), nil, struct{}{})
}

func TestHealthEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/health", handleHealth)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("health endpoint returned %d, want %d", w.Code, http.StatusOK)
	}
}
