package main

// MCPToolResult MCP 工具内部结果类型
type MCPToolResult struct {
	Content []MCPContent `json:"content"`
	IsError bool         `json:"is_error"`
}

// MCPContent MCP 内容项
type MCPContent struct {
	Type     string `json:"type"`               // "text" or "image"
	Text     string `json:"text,omitempty"`
	MimeType string `json:"mime_type,omitempty"` // for image
	Data     string `json:"data,omitempty"`      // base64 for image
}

// ErrorResponse HTTP API 错误响应
type ErrorResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// SuccessResponse HTTP API 成功响应
type SuccessResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// NewTextResult 创建文本结果
func NewTextResult(text string) *MCPToolResult {
	return &MCPToolResult{
		Content: []MCPContent{{Type: "text", Text: text}},
	}
}

// NewErrorResult 创建错误结果
func NewErrorResult(message string) *MCPToolResult {
	return &MCPToolResult{
		Content: []MCPContent{{Type: "text", Text: message}},
		IsError: true,
	}
}

// NewImageResult 创建图片结果
func NewImageResult(mimeType, base64Data string) *MCPToolResult {
	return &MCPToolResult{
		Content: []MCPContent{{Type: "image", MimeType: mimeType, Data: base64Data}},
	}
}
