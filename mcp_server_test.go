package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestMCPGetWithoutSessionReturnsFriendlyJSONRPCError(t *testing.T) {
	handler := NewMCPHTTPHandler(nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/mcp", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got := recorder.Header().Get("Mcp-Protocol-Version"); got != friendlyMCPProtocolVersion {
		t.Fatalf("Mcp-Protocol-Version = %q, want %q", got, friendlyMCPProtocolVersion)
	}
	if !strings.Contains(recorder.Body.String(), "GET /mcp 需要 Mcp-Session-Id") {
		t.Fatalf("unexpected body: %s", recorder.Body.String())
	}
}

func TestMCPPostWithoutSessionReturnsFriendlyJSONRPCError(t *testing.T) {
	handler := NewMCPHTTPHandler(nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{
		"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}
	}`))
	request.Header.Set("Content-Type", "application/json")

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "需要 Mcp-Session-Id header") {
		t.Fatalf("unexpected body: %s", recorder.Body.String())
	}
}

func TestMCPPostWithUnknownSessionReturnsFriendlyJSONRPCError(t *testing.T) {
	handler := NewMCPHTTPHandler(nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{
		"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}
	}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Mcp-Session-Id", "missing-session")

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "无效或已过期") {
		t.Fatalf("unexpected body: %s", recorder.Body.String())
	}
}

func TestMCPInitializeIncludesSessionIDInBody(t *testing.T) {
	handler := NewMCPHTTPHandler(nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{
		"jsonrpc":"2.0",
		"id":1,
		"method":"initialize",
		"params":{
			"protocolVersion":"2024-11-05",
			"capabilities":{},
			"clientInfo":{"name":"test-client","version":"1.0.0"}
		}
	}`))
	request.Header.Set("Content-Type", "application/json")

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	sessionID := recorder.Header().Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Fatal("initialize response is missing Mcp-Session-Id header")
	}
	var response struct {
		Result map[string]interface{} `json:"result"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid JSON response: %v; body=%s", err, recorder.Body.String())
	}
	if got, _ := response.Result["_session_id"].(string); got != sessionID {
		t.Fatalf("result._session_id = %q, want %q; body=%s", got, sessionID, recorder.Body.String())
	}

	listRecorder := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{
		"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}
	}`))
	listRequest.Header.Set("Content-Type", "application/json")
	listRequest.Header.Set("Mcp-Session-Id", sessionID)
	handler.ServeHTTP(listRecorder, listRequest)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("tools/list status = %d, body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	if strings.Contains(listRecorder.Body.String(), "_session_id") {
		t.Fatalf("non-initialize response should not be modified: %s", listRecorder.Body.String())
	}
}
