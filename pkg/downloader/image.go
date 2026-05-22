package downloader

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// 支持的图片扩展名
var validImageExtensions = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true,
	".gif": true, ".webp": true, ".bmp": true,
}

// ValidateImagePath 校验图片路径，支持本地文件和 HTTP URL
func ValidateImagePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("image path is empty")
	}

	// HTTP URL
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path, nil
	}

	// 本地文件
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", fmt.Errorf("image file not found: %s", path)
	}

	ext := strings.ToLower(filepath.Ext(path))
	if !validImageExtensions[ext] {
		return "", fmt.Errorf("unsupported image format: %s (supported: jpg, jpeg, png, gif, webp, bmp)", ext)
	}

	return path, nil
}

// DownloadImage 下载 HTTP 图片到临时目录
func DownloadImage(url string, tmpDir string) (string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to download image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	// 从 URL 提取文件名
	parts := strings.Split(url, "/")
	filename := parts[len(parts)-1]
	if idx := strings.Index(filename, "?"); idx > 0 {
		filename = filename[:idx]
	}
	if filename == "" {
		filename = "downloaded.jpg"
	}

	localPath := filepath.Join(tmpDir, filename)
	f, err := os.Create(localPath)
	if err != nil {
		return "", fmt.Errorf("failed to create file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", fmt.Errorf("failed to save image: %w", err)
	}

	return localPath, nil
}

// ProcessImages 处理图片列表：校验本地路径或下载远程图片
func ProcessImages(paths []string, tmpDir string) ([]string, error) {
	var result []string
	for _, p := range paths {
		validated, err := ValidateImagePath(p)
		if err != nil {
			return nil, err
		}

		if strings.HasPrefix(validated, "http://") || strings.HasPrefix(validated, "https://") {
			localPath, err := DownloadImage(validated, tmpDir)
			if err != nil {
				return nil, err
			}
			result = append(result, localPath)
		} else {
			result = append(result, validated)
		}
	}
	return result, nil
}
