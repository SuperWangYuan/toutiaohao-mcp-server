package toutiaohao

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/example/toutiaohao-mcp-server/configs"
	"github.com/example/toutiaohao-mcp-server/cookies"
	log "github.com/sirupsen/logrus"
)

// SaveMicroDraft 通过 HTTP API 保存微头条草稿
func SaveMicroDraft(ctx context.Context, content string, images []DraftImage, cookieStore cookies.Cookier) error {
	req := BuildDraftRequest(content, images)
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal draft request: %w", err)
	}

	// 构建 HTTP 请求
	httpReq, err := http.NewRequestWithContext(ctx, "POST", configs.SaveMicroDraftAPI, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json; charset=UTF-8")
	httpReq.Header.Set("Referer", "https://mp.toutiao.com/profile_v4/weitoutiao/publish")

	// 注入 Cookie
	data, err := cookieStore.LoadCookies()
	if err != nil || data == nil {
		return fmt.Errorf("no cookies available, please login first")
	}

	var cookieList []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(data, &cookieList); err != nil {
		return fmt.Errorf("invalid cookie format: %w", err)
	}
	for _, c := range cookieList {
		httpReq.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("draft save request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	log.Infof("Draft save response: %s", string(respBody))

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("draft save failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
