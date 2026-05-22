package browser

import (
	"os"
	"github.com/example/toutiaohao-mcp-server/cookies"
	log "github.com/sirupsen/logrus"
	headless_browser "github.com/xpzouying/headless_browser"
)

type browserConfig struct {
	binPath string
}

// Option 浏览器配置选项
type Option func(*browserConfig)

// WithBinPath 自定义浏览器路径
func WithBinPath(binPath string) Option {
	return func(c *browserConfig) {
		c.binPath = binPath
	}
}

// detectChromePath 自动检测 Chrome 浏览器路径
func detectChromePath() string {
	// 优先使用环境变量
	if p := os.Getenv("CHROME_BIN"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// macOS 默认路径
	defaultPaths := []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"/usr/bin/google-chrome",
		"/usr/bin/chromium",
		"/usr/bin/chromium-browser",
		"C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
		"C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe",
	}
	for _, p := range defaultPaths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// NewBrowser 创建浏览器实例
func NewBrowser(headless bool, options ...Option) *headless_browser.Browser {
	cfg := &browserConfig{}
	for _, opt := range options {
		opt(cfg)
	}

	opts := []headless_browser.Option{
		headless_browser.WithHeadless(headless),
	}

	binPath := cfg.binPath
	if binPath == "" {
		binPath = detectChromePath()
	}
	if binPath != "" {
		log.Infof("Using Chrome: %s", binPath)
		opts = append(opts, headless_browser.WithChromeBinPath(binPath))
	} else {
		log.Warn("Chrome binary not found, relying on launcher auto-detect")
	}

	// 加载 Cookie
	cookiePath := cookies.GetDefaultCookiePath()
	store := cookies.NewFileCookieStore(cookiePath)
	data, err := store.LoadCookies()
	if err != nil {
		log.Warnf("Failed to load cookies: %v", err)
	} else if data != nil {
		opts = append(opts, headless_browser.WithCookies(string(data)))
		log.Infof("Loaded cookies from %s", cookiePath)
	}

	return headless_browser.New(opts...)
}
