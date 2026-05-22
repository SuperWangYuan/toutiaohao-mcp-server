package toutiaohao

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/example/toutiaohao-mcp-server/configs"
	"github.com/example/toutiaohao-mcp-server/cookies"
	"github.com/go-rod/rod"
	log "github.com/sirupsen/logrus"
)

// loginSuccessURLFragments 登录成功后的 URL 片段列表
var loginSuccessURLFragments = []string{
	"www.toutiao.com/?is_new_connect",
	"mp.toutiao.com/profile",
	"creator.toutiao.com",
	"mp.toutiao.com/dashboard",
}

// IsLoginSuccessURL 判断 URL 是否表示登录成功
func IsLoginSuccessURL(url string) bool {
	for _, fragment := range loginSuccessURLFragments {
		if strings.Contains(url, fragment) {
			return true
		}
	}
	return false
}

// ValidateLoginRequest 校验登录请求参数
func ValidateLoginRequest(username, password string) error {
	if strings.TrimSpace(username) == "" {
		return fmt.Errorf("username is required")
	}
	if strings.TrimSpace(password) == "" {
		return fmt.Errorf("password is required")
	}
	return nil
}

// LoginAction 登录操作
type LoginAction struct {
	page        *rod.Page
	cookieStore cookies.Cookier
}

// NewLoginAction 创建登录操作
func NewLoginAction(page *rod.Page, cookieStore cookies.Cookier) *LoginAction {
	return &LoginAction{page: page, cookieStore: cookieStore}
}

// Login 执行登录流程
func (a *LoginAction) Login(ctx context.Context, username, password string) (*LoginResponse, error) {
	if err := ValidateLoginRequest(username, password); err != nil {
		return nil, err
	}

	// 导航到登录页
	log.Info("Navigating to login page...")
	if err := a.page.Navigate(configs.LoginPage); err != nil {
		return nil, fmt.Errorf("failed to navigate to login page: %w", err)
	}
	if err := a.page.WaitLoad(); err != nil {
		return nil, fmt.Errorf("failed to wait for page load: %w", err)
	}

	// 尝试自动填写账密
	log.Info("Attempting to fill credentials...")
	a.tryFillCredentials(username, password)

	// 轮询等待登录成功（支持人工补充验证码等）
	log.Info("Waiting for login to complete (you may need to complete captcha manually)...")
	if err := a.waitForLoginSuccess(ctx); err != nil {
		return nil, err
	}

	// 保存 Cookie
	if err := SaveBrowserCookies(a.page, a.cookieStore); err != nil {
		log.Warnf("Failed to save cookies: %v", err)
	}

	return &LoginResponse{
		Success: true,
		Message: "Login successful",
	}, nil
}

// tryFillCredentials 尝试自动填写账密（不报错，因为页面选择器可能变化）
func (a *LoginAction) tryFillCredentials(username, password string) {
	if el, sel, err := findElement(a.page, 2*time.Second, LoginUsernameSelectors); err == nil && el != nil {
		_ = inputText(el, username)
		log.Infof("Username filled using selector: %s", sel)
	}

	if el, sel, err := findElement(a.page, 2*time.Second, LoginPasswordSelectors); err == nil && el != nil {
		_ = inputText(el, password)
		log.Infof("Password filled using selector: %s", sel)
	}

	if err := clickFirst(a.page, 2*time.Second, LoginSubmitButtonSelectors, "login button"); err != nil {
		log.Debugf("Login button not clicked automatically: %v", err)
	}
}

// waitForLoginSuccess 轮询等待登录成功
func (a *LoginAction) waitForLoginSuccess(ctx context.Context) error {
	timeout := 300 * time.Second
	interval := 1 * time.Second
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		info, err := a.page.Info()
		if err == nil && IsLoginSuccessURL(info.URL) {
			log.Infof("Login success detected, URL: %s", info.URL)
			return nil
		}

		elapsed := time.Since(deadline.Add(-timeout))
		if int(elapsed.Seconds())%30 == 0 && elapsed.Seconds() > 0 {
			log.Infof("Still waiting for login... (%d seconds elapsed)", int(elapsed.Seconds()))
		}

		time.Sleep(interval)
	}

	return fmt.Errorf("login timeout after %v", timeout)
}

// SaveBrowserCookies 从当前浏览器页面保存 Cookie 到本地
func SaveBrowserCookies(page *rod.Page, store cookies.Cookier) error {
	browserCookies, err := page.Cookies(nil)
	if err != nil {
		return fmt.Errorf("failed to get cookies: %w", err)
	}

	type cookieEntry struct {
		Name     string `json:"name"`
		Value    string `json:"value"`
		Domain   string `json:"domain"`
		Path     string `json:"path"`
		Expires  int64  `json:"expires,omitempty"`
		HTTPOnly bool   `json:"httpOnly"`
		Secure   bool   `json:"secure"`
	}

	var entries []cookieEntry
	for _, c := range browserCookies {
		entries = append(entries, cookieEntry{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Expires:  int64(c.Expires),
			HTTPOnly: c.HTTPOnly,
			Secure:   c.Secure,
		})
	}

	data, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("failed to marshal cookies: %w", err)
	}

	return store.SaveCookies(data)
}

// EnsureLogin 确保当前页面处于已登录状态。
// 如果发现未登录或会话已过期跳转到登录页，它将通过日志提醒用户，并等待用户手动在弹出的浏览器上扫码登录。
// 只要用户在 5 分钟超时时间内扫码登录成功，它将自动捕获新 Cookie 并保存，之后继续之前的操作。
func EnsureLogin(page *rod.Page, cookieStore cookies.Cookier) error {
	info, err := page.Info()
	if err != nil {
		return fmt.Errorf("failed to get page info: %w", err)
	}

	if strings.Contains(info.URL, "login") || strings.Contains(info.URL, "auth") {
		log.Warn("=================================================================")
		log.Warn("【安全提示】检测到当前未登录或会话已失效！")
		log.Warn("请在弹出的 Chrome 浏览器窗口中，及时使用手机完成扫码登录...")
		log.Warn("=================================================================")

		timeout := 300 * time.Second
		interval := 1 * time.Second
		deadline := time.Now().Add(timeout)

		for time.Now().Before(deadline) {
			currentInfo, err := page.Info()
			if err == nil {
				// 头条号的首页或发布页一般是 mp.toutiao.com/profile 或是 mp.toutiao.com/profile_v4/xxx
				if IsLoginSuccessURL(currentInfo.URL) || 
				   (!strings.Contains(currentInfo.URL, "login") && !strings.Contains(currentInfo.URL, "auth") && strings.Contains(currentInfo.URL, "mp.toutiao.com")) {
					log.Info("检测到扫码登录成功！")
					
					// 延迟一秒等待 Cookie 完全写入浏览器内存
					time.Sleep(1 * time.Second)
					
					// 自动回写 Cookie 到本地
					if err := SaveBrowserCookies(page, cookieStore); err != nil {
						log.Warnf("自动保存新 Cookie 失败: %v", err)
					} else {
						log.Info("新 Cookie 已成功保存，继续执行原自动化操作。")
					}
					return nil
				}
			}
			time.Sleep(interval)
		}
		return fmt.Errorf("扫码登录超时（已等待 5 分钟），请重新运行并及时扫码")
	}

	return nil
}

// CheckLoginStatus 检查登录状态（通过 HTTP 请求）
func CheckLoginStatus(cookieStore cookies.Cookier) (*LoginStatusResponse, error) {
	data, err := cookieStore.LoadCookies()
	if err != nil || data == nil {
		return &LoginStatusResponse{
			LoggedIn: false,
			Message:  "No cookies found",
		}, nil
	}

	// 解析 Cookie
	type cookieEntry struct {
		Name    string `json:"name"`
		Value   string `json:"value"`
		Expires int64  `json:"expires,omitempty"`
	}
	var cookieList []cookieEntry
	if err := json.Unmarshal(data, &cookieList); err != nil {
		return &LoginStatusResponse{
			LoggedIn: false,
			Message:  "Invalid cookie format",
		}, nil
	}

	// 检查是否有关键 Cookie 已过期
	now := time.Now().Unix()
	hasSessionCookie := false
	for _, c := range cookieList {
		// 检查过期时间（Expires > 0 表示设置了过期时间）
		if c.Expires > 0 && c.Expires < now {
			log.Debugf("Cookie %s has expired (expires=%d, now=%d)", c.Name, c.Expires, now)
			continue
		}
		// 检查是否存在关键的会话 Cookie
		if c.Name == "sessionid" || c.Name == "sso_uid" || c.Name == "passport_csrf_token" || c.Name == "sid_guard" {
			hasSessionCookie = true
		}
	}

	if !hasSessionCookie {
		return &LoginStatusResponse{
			LoggedIn: false,
			Message:  "Session cookies missing or expired",
		}, nil
	}

	// 构建带 Cookie 的 HTTP 请求
	client := &http.Client{
		Timeout: 10 * time.Second,
		// 不自动跟随重定向，以便手动检测
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequest("GET", configs.Homepage, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	for _, c := range cookieList {
		// 跳过已过期的 Cookie
		if c.Expires > 0 && c.Expires < now {
			continue
		}
		req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	}

	resp, err := client.Do(req)
	if err != nil {
		return &LoginStatusResponse{
			LoggedIn: false,
			Message:  fmt.Sprintf("Request failed: %v", err),
		}, nil
	}
	defer resp.Body.Close()

	// 检查 HTTP 状态码
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return &LoginStatusResponse{
			LoggedIn: false,
			Message:  fmt.Sprintf("Server returned %d, session likely expired", resp.StatusCode),
		}, nil
	}

	// 检查是否被重定向到登录页
	location := resp.Header.Get("Location")
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		if strings.Contains(location, "login") || strings.Contains(location, "auth") {
			return &LoginStatusResponse{
				LoggedIn: false,
				Message:  fmt.Sprintf("Redirected to login page: %s", location),
			}, nil
		}
	}

	// 对于 200 响应，也检查最终 URL
	finalURL := resp.Request.URL.String()
	if strings.Contains(finalURL, "login") || strings.Contains(finalURL, "auth") {
		return &LoginStatusResponse{
			LoggedIn: false,
			Message:  "Redirected to login page",
		}, nil
	}

	return &LoginStatusResponse{
		LoggedIn: true,
		Message:  "Logged in",
	}, nil
}
