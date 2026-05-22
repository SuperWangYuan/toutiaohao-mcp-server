package configs

import "os"

// BrowserConfig 浏览器配置
type BrowserConfig struct {
	Headless   bool
	BinPath    string
	ProxyURL   string
	UserAgent  string
}

// DefaultBrowserConfig 返回默认浏览器配置
func DefaultBrowserConfig() BrowserConfig {
	return BrowserConfig{
		Headless:  false,
		ProxyURL:  os.Getenv("TOUTIAOHAO_PROXY"),
		UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	}
}
