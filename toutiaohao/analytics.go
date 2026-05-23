package toutiaohao

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/example/toutiaohao-mcp-server/configs"
	"github.com/example/toutiaohao-mcp-server/cookies"
	"github.com/go-rod/rod"
	log "github.com/sirupsen/logrus"
)

var validReportTypes = map[string]bool{
	"daily": true, "weekly": true, "monthly": true,
}

// ValidateArticleStats 校验文章统计参数
func ValidateArticleStats(articleID string) error {
	if strings.TrimSpace(articleID) == "" {
		return fmt.Errorf("article_id is required")
	}
	return nil
}

// NormalizeReportType 规范化报告类型（空值默认为 weekly）
func NormalizeReportType(rt string) string {
	if rt == "" {
		return "weekly"
	}
	return rt
}

// ValidateReportType 校验报告类型
func ValidateReportType(rt string) error {
	if !validReportTypes[rt] {
		return fmt.Errorf("invalid report_type '%s', valid values: daily, weekly, monthly", rt)
	}
	return nil
}

// AccountOverview 账户概览数据
type AccountOverview struct {
	Followers   int    `json:"followers"`
	TotalReads  int    `json:"total_reads"`
	TotalLikes  int    `json:"total_likes"`
	TotalShares int    `json:"total_shares"`
	Revenue     string `json:"revenue,omitempty"`
	RawData     string `json:"raw_data,omitempty"`
}

// ArticleStats 文章统计数据
type ArticleStats struct {
	ArticleID    string `json:"article_id"`
	ReadCount    int    `json:"read_count"`
	LikeCount    int    `json:"like_count"`
	CommentCount int    `json:"comment_count"`
	ShareCount   int    `json:"share_count"`
}

// Report 分析报告
type Report struct {
	Type            string           `json:"type"`
	Period          string           `json:"period"`
	Summary         string           `json:"summary"`
	AccountOverview *AccountOverview `json:"account_overview,omitempty"`
}

// GetAccountOverview 获取账户概览（通过浏览器自动化抓取创作者后台首页数据）
func GetAccountOverview(ctx context.Context, page *rod.Page, cookieStore cookies.Cookier) (*AccountOverview, error) {
	log.Info("开始获取账户概览数据（浏览器自动化方式）...")

	// 注入 Cookie
	if err := injectBrowserCookies(page, cookieStore); err != nil {
		return nil, fmt.Errorf("注入 Cookie 失败: %w", err)
	}

	// 导航到创作者后台首页
	log.Infof("导航到创作者后台首页: %s", configs.Homepage)
	if err := page.Navigate(configs.Homepage); err != nil {
		return nil, fmt.Errorf("导航到首页失败: %w", err)
	}
	if err := page.Timeout(15 * time.Second).WaitLoad(); err != nil {
		return nil, fmt.Errorf("等待页面加载失败: %w", err)
	}

	// 检查并处理登录状态 (包含扫码等待)
	if err := EnsureLogin(page, cookieStore); err != nil {
		return nil, err
	}

	// 再次导航到首页以确保处于已登录的状态渲染
	if err := page.Navigate(configs.Homepage); err != nil {
		return nil, fmt.Errorf("导航到首页失败: %w", err)
	}
	_ = page.Timeout(10 * time.Second).WaitLoad()

	// 等待页面渲染完成
	time.Sleep(3 * time.Second)

	info, err := page.Info()
	if err != nil {
		return nil, fmt.Errorf("获取页面信息失败: %w", err)
	}
	log.Infof("当前页面 URL: %s", info.URL)

	// 保存截图方便调试
	_ = page.MustScreenshot("./screenshot_analytics.png")
	log.Info("已保存分析页截图到 screenshot_analytics.png")

	// 使用 JavaScript 从页面中提取所有可见的数据信息
	result, err := page.Timeout(10 * time.Second).Eval(`() => {
		// 收集页面上的数据面板信息
		const data = {
			followers: 0,
			totalReads: 0,
			totalLikes: 0,
			totalShares: 0,
			rawTexts: []
		};

		// 策略 1：查找包含数字的 data-card / stat 区域
		const statSelectors = [
			'.data-card', '.stat-card', '.overview-card',
			'.data-item', '.stat-item', '.overview-item',
			'[class*="stat"]', '[class*="data"]', '[class*="overview"]',
			'[class*="fans"]', '[class*="follower"]',
			'[class*="read"]', '[class*="view"]',
			'[class*="like"]', '[class*="praise"]',
			'[class*="share"]', '[class*="forward"]',
			'.index-module', '[class*="index-module"]',
			'[class*="card"]', '[class*="panel"]',
			'[class*="dashboard"]', '[class*="summary"]'
		];

		for (const sel of statSelectors) {
			try {
				const els = document.querySelectorAll(sel);
				for (const el of els) {
					const text = el.innerText || '';
					if (text.trim().length > 0 && text.trim().length < 500) {
						data.rawTexts.push(text.trim());
					}
				}
			} catch(e) {}
		}

		// 策略 2：收集所有页面可见文本中包含数字的关键区域
		const allElements = document.querySelectorAll('h1, h2, h3, h4, h5, span, div, p, strong, em, a, li, dt, dd, label');
		const keywords = ['粉丝', '关注', '阅读', '播放', '展现', '点赞', '赞', '转发', '分享', '评论', '收藏', '作品', '文章', '微头条', '昨日', '今日', '总', '累计'];
		
		for (const el of allElements) {
			const text = (el.innerText || '').trim();
			if (text.length > 0 && text.length < 200) {
				for (const kw of keywords) {
					if (text.includes(kw)) {
						data.rawTexts.push(text);
						break;
					}
				}
			}
		}

		// 去重
		data.rawTexts = [...new Set(data.rawTexts)];

		// 尝试从文本中提取数字（针对头条创作者后台的 "标题\n数字" 格式）
		function extractNumber(texts, keywords, excludeKeywords) {
			excludeKeywords = excludeKeywords || [];
			let bestMatch = 0;

			for (const text of texts) {
				// 排除包含排除关键词的文本
				let excluded = false;
				for (const ek of excludeKeywords) {
					if (text.includes(ek)) { excluded = true; break; }
				}
				if (excluded) continue;

				for (const kw of keywords) {
					if (!text.includes(kw)) continue;

					// 策略 1: 匹配 "关键词\n数字" 或 "关键词 数字" 格式（紧邻）
					const regex1 = new RegExp(kw + '[\\s\\n]*([\\d,]+(?:\\.\\d+)?)');
					const m1 = text.match(regex1);
					if (m1) {
						const num = parseFloat(m1[1].replace(/,/g, ''));
						if (num > bestMatch) bestMatch = num;
					}

					// 策略 2: 分行匹配 - 找到关键词所在行的下一行数字
					const lines = text.split('\n');
					for (let i = 0; i < lines.length - 1; i++) {
						if (lines[i].includes(kw)) {
							const nextLine = lines[i + 1].trim();
							const m2 = nextLine.match(/^([\d,]+(?:\.\d+)?)/);
							if (m2) {
								const num = parseFloat(m2[1].replace(/,/g, ''));
								if (num > bestMatch) bestMatch = num;
							}
						}
					}
				}
			}
			return Math.floor(bestMatch);
		}

		// 提取收益金额
		function extractMoney(texts) {
			for (const text of texts) {
				if (text.includes('累计收益') || text.includes('收益')) {
					const m = text.match(/([\d,]+(?:\.\d+)?)元/);
					if (m) return m[1];
				}
			}
			return '0';
		}

		// 转义正则特殊字符
		function escapeRegex(s) {
			return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
		}

		// 重写 extractNumber 以使用字符串匹配而非正则匹配关键词
		function extractNumberV2(texts, keywords, excludeKeywords) {
			excludeKeywords = excludeKeywords || [];
			let bestMatch = 0;

			for (const text of texts) {
				let excluded = false;
				for (const ek of excludeKeywords) {
					if (text.includes(ek)) { excluded = true; break; }
				}
				if (excluded) continue;

				for (const kw of keywords) {
					if (!text.includes(kw)) continue;

					// 分行匹配 - 找到关键词所在行的下一行数字
					const lines = text.split('\n');
					for (let i = 0; i < lines.length - 1; i++) {
						if (lines[i].includes(kw)) {
							const nextLine = lines[i + 1].trim();
							const m = nextLine.match(/^([\d,]+(?:\.\d+)?)/);
							if (m) {
								const num = parseFloat(m[1].replace(/,/g, ''));
								if (num > bestMatch) bestMatch = num;
							}
						}
					}

					// 同行匹配 - 关键词后紧跟数字
					const idx = text.indexOf(kw);
					if (idx >= 0) {
						const after = text.substring(idx + kw.length);
						const m = after.match(/^\s*([\d,]+(?:\.\d+)?)/);
						if (m) {
							const num = parseFloat(m[1].replace(/,/g, ''));
							if (num > bestMatch) bestMatch = num;
						}
					}
				}
			}
			return Math.floor(bestMatch);
		}

		data.followers = extractNumberV2(data.rawTexts, ['粉丝数', '粉丝']);
		data.totalReads = extractNumberV2(data.rawTexts, ['总阅读(播放)量', '总阅读', '阅读(播放)量']);
		data.totalLikes = extractNumberV2(data.rawTexts, ['总点赞', '获赞']);
		data.totalShares = extractNumberV2(data.rawTexts, ['总转发', '转发', '分享']);
		data.revenue = extractMoney(data.rawTexts);

		return data;
	}`)

	if err != nil {
		return nil, fmt.Errorf("从页面提取数据失败: %w", err)
	}

	// 解析 JavaScript 返回的 JSON 结果
	jsonBytes, err := json.Marshal(result.Value)
	if err != nil {
		return nil, fmt.Errorf("序列化页面数据失败: %w", err)
	}
	jsonStr := string(jsonBytes)
	log.Infof("页面提取的原始数据: %s", jsonStr)

	var pageData struct {
		Followers   int      `json:"followers"`
		TotalReads  int      `json:"totalReads"`
		TotalLikes  int      `json:"totalLikes"`
		TotalShares int      `json:"totalShares"`
		Revenue     string   `json:"revenue"`
		RawTexts    []string `json:"rawTexts"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &pageData); err != nil {
		return nil, fmt.Errorf("解析页面数据失败: %w", err)
	}

	rawDataStr := strings.Join(pageData.RawTexts, " | ")
	log.Infof("提取结果: 粉丝=%d, 阅读=%d, 点赞=%d, 分享=%d, 收益=%s元", pageData.Followers, pageData.TotalReads, pageData.TotalLikes, pageData.TotalShares, pageData.Revenue)
	log.Infof("页面原始文本: %s", rawDataStr)

	return &AccountOverview{
		Followers:   pageData.Followers,
		TotalReads:  pageData.TotalReads,
		TotalLikes:  pageData.TotalLikes,
		TotalShares: pageData.TotalShares,
		Revenue:     pageData.Revenue,
		RawData:     rawDataStr,
	}, nil
}

// GetArticleStats 获取文章统计（优先通过文章列表获取，避免废弃 API 的 404 报错）
func GetArticleStats(ctx context.Context, articleID string, cookieStore cookies.Cookier) (*ArticleStats, error) {
	if err := ValidateArticleStats(articleID); err != nil {
		return nil, err
	}

	log.Infof("正在通过获取文章列表匹配 article_id = %s 的统计数据...", articleID)

	// 最多查询 2 页列表，每页 50 篇（覆盖最近 100 篇文章）
	for page := 1; page <= 2; page++ {
		params := &ArticleListParams{
			Page:     page,
			PageSize: 50,
			Status:   "all",
		}
		listResp, err := GetArticleList(ctx, params, cookieStore)
		if err != nil {
			log.Warnf("通过文章列表获取统计时发生错误（第 %d 页）: %v", page, err)
			break
		}
		if listResp == nil || len(listResp.Articles) == 0 {
			break
		}

		for _, item := range listResp.Articles {
			if item.ArticleID == articleID {
				log.Infof("在文章列表中成功匹配到 article_id = %s", articleID)
				return &ArticleStats{
					ArticleID:    item.ArticleID,
					ReadCount:    item.ReadCount,
					LikeCount:    item.DiggCount,
					CommentCount: item.CommentCount,
					ShareCount:   0, // 列表不返回具体的 share_count，默认赋 0
				}, nil
			}
		}

		if len(listResp.Articles) < 50 {
			break // 无更多文章
		}
	}

	// 退路：如果没在近 100 篇文章中匹配到，则尝试原先的 API 接口
	log.Warnf("在最近的列表中未找到匹配的 article_id = %s，尝试原 API 请求...", articleID)
	url := fmt.Sprintf("%s?article_id=%s", configs.ArticleStatsAPI, articleID)
	body, err := doAuthenticatedGet(ctx, url, cookieStore)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch article stats (and not found in latest article list): %w", err)
	}

	var resp struct {
		Data struct {
			ReadCount    int `json:"read_count"`
			LikeCount    int `json:"like_count"`
			CommentCount int `json:"comment_count"`
			ShareCount   int `json:"share_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse stats response: %w", err)
	}

	return &ArticleStats{
		ArticleID:    articleID,
		ReadCount:    resp.Data.ReadCount,
		LikeCount:    resp.Data.LikeCount,
		CommentCount: resp.Data.CommentCount,
		ShareCount:   resp.Data.ShareCount,
	}, nil
}

// GenerateReport 生成分析报告
func GenerateReport(ctx context.Context, reportType string, page *rod.Page, cookieStore cookies.Cookier) (*Report, error) {
	rt := NormalizeReportType(reportType)
	if err := ValidateReportType(rt); err != nil {
		return nil, err
	}

	overview, err := GetAccountOverview(ctx, page, cookieStore)
	if err != nil {
		return nil, fmt.Errorf("failed to get account overview: %w", err)
	}

	now := time.Now()
	var period string
	switch rt {
	case "daily":
		period = now.Format("2006-01-02")
	case "weekly":
		period = fmt.Sprintf("%s ~ %s", now.AddDate(0, 0, -7).Format("2006-01-02"), now.Format("2006-01-02"))
	case "monthly":
		period = now.Format("2006-01")
	}

	return &Report{
		Type:            rt,
		Period:          period,
		Summary:         fmt.Sprintf("%s report: %d followers, %d reads, %d likes", rt, overview.Followers, overview.TotalReads, overview.TotalLikes),
		AccountOverview: overview,
	}, nil
}


// injectBrowserCookies 将 cookieStore 中的 Cookie 注入到 rod 浏览器页面中
// 如果没有可用 Cookie，会直接导航到目标页面（自动跳转到登录页供用户手动登录）
func injectBrowserCookies(page *rod.Page, cookieStore cookies.Cookier) error {
	data, err := cookieStore.LoadCookies()
	if err != nil || data == nil {
		log.Warn("无可用 Cookie，直接导航到头条后台（将自动跳转到登录页）")
		// 直接导航到目标域名，没登录会自动跳转登录页
		if err := page.Navigate("https://mp.toutiao.com"); err != nil {
			return fmt.Errorf("导航到头条域名失败: %w", err)
		}
		time.Sleep(2 * time.Second)
		return nil
	}

	type cookieEntry struct {
		Name     string  `json:"name"`
		Value    string  `json:"value"`
		Domain   string  `json:"domain"`
		Path     string  `json:"path"`
		Expires  float64 `json:"expires"`
		HTTPOnly bool    `json:"httpOnly"`
		Secure   bool    `json:"secure"`
	}

	var entries []cookieEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("failed to parse cookies: %w", err)
	}

	// 先导航到目标域名，才能设置 Cookie
	if err := page.Navigate("https://mp.toutiao.com"); err != nil {
		return fmt.Errorf("导航到头条域名失败: %w", err)
	}
	time.Sleep(1 * time.Second)

	// 通过 CDP 协议设置 Cookie
	for _, c := range entries {
		_, err := page.Eval(`(name, value, domain, path) => {
			document.cookie = name + '=' + value + '; domain=' + domain + '; path=' + path;
		}`, c.Name, c.Value, c.Domain, c.Path)
		if err != nil {
			log.Debugf("通过 JS 设置 Cookie %s 失败: %v", c.Name, err)
		}
	}

	log.Infof("已注入 %d 个 Cookie", len(entries))
	return nil
}
