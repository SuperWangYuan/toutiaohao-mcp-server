package main

import (
	"testing"
)

func TestLoginHandlerArgsValidation_EmptyUsername(t *testing.T) {
	args := map[string]interface{}{
		"username": "",
		"password": "pass",
	}
	result := handleLoginArgsValidation(args)
	if result == nil {
		t.Fatal("expected error result for empty username")
	}
	if !result.IsError {
		t.Error("expected IsError=true")
	}
}

func TestLoginHandlerArgsValidation_EmptyPassword(t *testing.T) {
	args := map[string]interface{}{
		"username": "user",
		"password": "",
	}
	result := handleLoginArgsValidation(args)
	if result == nil {
		t.Fatal("expected error result for empty password")
	}
	if !result.IsError {
		t.Error("expected IsError=true")
	}
}

func TestLoginHandlerArgsValidation_Valid(t *testing.T) {
	args := map[string]interface{}{
		"username": "user",
		"password": "pass",
	}
	result := handleLoginArgsValidation(args)
	if result != nil {
		t.Errorf("expected nil for valid args, got error: %v", result)
	}
}

func TestLoginToolSchema(t *testing.T) {
	service := NewToutiaoService(nil)
	server := NewAppServer(service)

	// 验证工具已注册 - InitMCPServer 在 NewAppServer 中调用
	if server.mcpServer == nil {
		t.Fatal("mcpServer is nil")
	}
}
