package cookies

import (
	"os"
	"path/filepath"
	"runtime"
)


// Cookier Cookie 持久化接口
type Cookier interface {
	LoadCookies() ([]byte, error)
	SaveCookies(data []byte) error
	DeleteCookies() error
	GetFilePath() string
}

// FileCookieStore 基于本地文件的 Cookie 持久化
type FileCookieStore struct {
	filePath string
}

// NewFileCookieStore 创建 FileCookieStore
func NewFileCookieStore(filePath string) *FileCookieStore {
	return &FileCookieStore{filePath: filePath}
}

// LoadCookies 从文件加载 Cookie，文件不存在返回 nil
func (s *FileCookieStore) LoadCookies() ([]byte, error) {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}

// SaveCookies 保存 Cookie 到文件
func (s *FileCookieStore) SaveCookies(data []byte) error {
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(s.filePath, data, 0644)
}

// DeleteCookies 删除 Cookie 文件
func (s *FileCookieStore) DeleteCookies() error {
	err := os.Remove(s.filePath)
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	return err
}

// GetFilePath 返回 Cookie 文件路径
func (s *FileCookieStore) GetFilePath() string {
	return s.filePath
}

// GetDefaultCookiePath 返回默认 Cookie 文件路径
func GetDefaultCookiePath() string {
	if p := os.Getenv("TOUTIAOHAO_COOKIES_PATH"); p != "" {
		return p
	}

	// 1. 如果当前工作目录下已经存在 cookies.json，直接使用它
	if _, err := os.Stat("cookies.json"); err == nil {
		return "cookies.json"
	}

	// 2. 尝试根据当前源文件定位到项目根目录下的 cookies.json
	if _, filename, _, ok := runtime.Caller(0); ok {
		projDir := filepath.Dir(filepath.Dir(filename))
		p := filepath.Join(projDir, "cookies.json")
		return p
	}

	return "cookies.json"
}
