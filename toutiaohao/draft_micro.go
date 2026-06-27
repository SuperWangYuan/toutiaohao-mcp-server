package toutiaohao

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/example/toutiaohao-mcp-server/configs"
	"github.com/example/toutiaohao-mcp-server/cookies"
	log "github.com/sirupsen/logrus"
)

// SaveMicroDraftWithImagePaths 上传本地/网络图片后保存微头条草稿。
func SaveMicroDraftWithImagePaths(ctx context.Context, content string, imagePaths []string, cookieStore cookies.Cookier) error {
	var draftImages []DraftImage
	for i, imagePath := range imagePaths {
		localPath, cleanup, err := downloadImageToTemp(imagePath)
		if err != nil {
			return fmt.Errorf("准备微头条草稿图片 %d/%d 失败: %w", i+1, len(imagePaths), err)
		}
		draftImage, err := uploadMicroDraftImage(ctx, localPath, cookieStore)
		cleanup()
		if err != nil {
			return fmt.Errorf("上传微头条草稿图片 %d/%d 失败: %w", i+1, len(imagePaths), err)
		}
		draftImages = append(draftImages, draftImage)
	}
	return SaveMicroDraft(ctx, content, draftImages, cookieStore)
}

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

func uploadMicroDraftImage(ctx context.Context, localPath string, cookieStore cookies.Cookier) (DraftImage, error) {
	stat, err := os.Stat(localPath)
	if err != nil {
		return DraftImage{}, fmt.Errorf("图片文件不可读: %w", err)
	}
	if stat.Size() <= 0 {
		return DraftImage{}, fmt.Errorf("图片文件为空: %s", localPath)
	}
	width, height, mimeType := inspectLocalImage(localPath)
	if mimeType == "" {
		mimeType = "image/jpeg"
	}

	data, err := cookieStore.LoadCookies()
	if err != nil || data == nil {
		return DraftImage{}, fmt.Errorf("no cookies available, please login first")
	}

	var lastErr error
	for _, fieldName := range []string{"image", "file"} {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, err := writer.CreateFormFile(fieldName, filepath.Base(localPath))
		if err != nil {
			return DraftImage{}, err
		}
		file, err := os.Open(localPath)
		if err != nil {
			return DraftImage{}, err
		}
		_, copyErr := io.Copy(part, file)
		_ = file.Close()
		if copyErr != nil {
			return DraftImage{}, copyErr
		}
		_ = writer.WriteField("type", "image")
		_ = writer.WriteField("source", "weitoutiao")
		if err := writer.Close(); err != nil {
			return DraftImage{}, err
		}

		req, err := http.NewRequestWithContext(ctx, "POST", configs.UploadImageAPI, body)
		if err != nil {
			return DraftImage{}, err
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Referer", "https://mp.toutiao.com/profile_v4/weitoutiao/publish")
		injectCookies(req, data)

		client := &http.Client{Timeout: 60 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		respBody, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		log.Infof("微头条草稿图片上传响应 field=%s status=%d body=%s", fieldName, resp.StatusCode, truncateStr(string(respBody), 500))
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("status=%d body=%s", resp.StatusCode, truncateStr(string(respBody), 300))
			continue
		}
		draftImage, err := parseDraftImageUploadResponse(respBody, width, height, mimeType, stat.Size())
		if err != nil {
			lastErr = err
			continue
		}
		return draftImage, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("未能上传图片")
	}
	return DraftImage{}, lastErr
}

func inspectLocalImage(localPath string) (int, int, string) {
	file, err := os.Open(localPath)
	if err != nil {
		return 0, 0, ""
	}
	defer file.Close()
	cfg, format, err := image.DecodeConfig(file)
	if err != nil {
		ext := strings.ToLower(filepath.Ext(localPath))
		switch ext {
		case ".png":
			return 0, 0, "image/png"
		case ".gif":
			return 0, 0, "image/gif"
		case ".webp":
			return 0, 0, "image/webp"
		default:
			return 0, 0, "image/jpeg"
		}
	}
	mimeType := "image/jpeg"
	switch strings.ToLower(format) {
	case "png":
		mimeType = "image/png"
	case "gif":
		mimeType = "image/gif"
	case "jpeg", "jpg":
		mimeType = "image/jpeg"
	}
	return cfg.Width, cfg.Height, mimeType
}

func parseDraftImageUploadResponse(body []byte, width, height int, mimeType string, fileSize int64) (DraftImage, error) {
	var root interface{}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return DraftImage{}, fmt.Errorf("图片上传响应不是 JSON: %w", err)
	}
	code := findFirstNumber(root, "code", "err_no", "errno", "status_code")
	if code != "" && code != "0" && code != "200" {
		return DraftImage{}, fmt.Errorf("图片上传失败 code=%s body=%s", code, truncateStr(string(body), 300))
	}
	url := findFirstString(root, "url", "image_url", "download_url", "display_url")
	webURI := findFirstString(root, "web_uri", "webUri", "weburi")
	uri := findFirstString(root, "uri", "image_uri", "imageUri", "tos_uri", "tosUri")
	if uri == "" {
		uri = webURI
	}
	if webURI == "" {
		webURI = uri
	}
	if url == "" && uri == "" && webURI == "" {
		return DraftImage{}, fmt.Errorf("图片上传响应缺少 url/uri/web_uri: %s", truncateStr(string(body), 500))
	}
	if width == 0 {
		width = intValueOrZero(findFirstNumber(root, "width", "image_width"))
	}
	if height == 0 {
		height = intValueOrZero(findFirstNumber(root, "height", "image_height"))
	}
	if mimeType == "" {
		mimeType = findFirstString(root, "mime_type", "mimeType")
	}
	if fileSize == 0 {
		fileSize = int64(intValueOrZero(findFirstNumber(root, "file_size", "fileSize", "size")))
	}
	return DraftImage{
		URL:      url,
		WebURI:   webURI,
		URI:      uri,
		Width:    width,
		Height:   height,
		MimeType: mimeType,
		FileSize: fileSize,
	}, nil
}

func findFirstString(value interface{}, keys ...string) string {
	keySet := map[string]bool{}
	for _, key := range keys {
		keySet[strings.ToLower(key)] = true
	}
	var walk func(interface{}) string
	walk = func(v interface{}) string {
		switch x := v.(type) {
		case map[string]interface{}:
			for key, val := range x {
				if keySet[strings.ToLower(key)] {
					if s := valueToString(val); s != "" {
						return s
					}
				}
			}
			for _, val := range x {
				if s := walk(val); s != "" {
					return s
				}
			}
		case []interface{}:
			for _, val := range x {
				if s := walk(val); s != "" {
					return s
				}
			}
		}
		return ""
	}
	return walk(value)
}

func findFirstNumber(value interface{}, keys ...string) string {
	return findFirstString(value, keys...)
}

func intValueOrZero(value string) int {
	if value == "" {
		return 0
	}
	var n int
	_, _ = fmt.Sscanf(value, "%d", &n)
	return n
}
