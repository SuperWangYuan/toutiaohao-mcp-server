package browser

import (
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

// NewBrowser 创建浏览器实例
func NewBrowser(headless bool, options ...Option) *headless_browser.Browser {
	cfg := &browserConfig{}
	for _, opt := range options {
		opt(cfg)
	}

	opts := []headless_browser.Option{
		headless_browser.WithHeadless(headless),
	}

	if cfg.binPath != "" {
		opts = append(opts, headless_browser.WithChromeBinPath(cfg.binPath))
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
