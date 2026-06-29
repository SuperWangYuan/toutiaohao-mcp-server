package configs

// 内容限制
const (
	// MaxTitleLength 使用头条标题权重：中文和标点计 1，ASCII 字母/数字计 0.5，空白不计。
	MaxTitleLength      = 30
	MaxContentLength    = 50000
	MaxWeitoutiaoLength = 2000
	MaxImagesPerPost    = 9
	MaxImageSize        = 10 * 1024 * 1024 // 10MB
)
