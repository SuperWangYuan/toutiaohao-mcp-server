package toutiaohao

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/example/toutiaohao-mcp-server/browser"
	"github.com/example/toutiaohao-mcp-server/cookies"
	log "github.com/sirupsen/logrus"
)

func TestPublishArticleManual(t *testing.T) {
	// 动态创建测试插图目录与 dummy 临时图片文件
	testdataDir := "./testdata"
	testImagePath := filepath.Join(testdataDir, "test_cloud_computing.png")
	if err := os.MkdirAll(testdataDir, 0755); err != nil {
		t.Fatalf("创建临时测试目录失败: %v", err)
	}
	// 写入一个 100 字节的伪 PNG 数据，确保图片能够正常上传
	dummyPNG := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15c4\x00\x00\x00\rIDATx\x9cc`\x00\x00\x00\x02\x00\x01H\xaf\xa4q\x00\x00\x00\x00IEND\xaeB`\x82")
	if err := os.WriteFile(testImagePath, dummyPNG, 0644); err != nil {
		t.Fatalf("创建临时测试图片失败: %v", err)
	}
	// 测试结束时自动删除
	defer func() {
		_ = os.RemoveAll(testdataDir)
	}()

	// 设置环境变量以便内部 browser 加载根目录下的 cookies.json
	os.Setenv("TOUTIAOHAO_COOKIES_PATH", "../cookies.json")

	// 这个测试需要本地有有效的 cookies.json 并且处于联网状态
	if _, err := os.Stat("../cookies.json"); os.IsNotExist(err) {
		t.Skip("跳过集成测试：未找到 cookies.json")
	}

	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})
	log.Info("开始图文插图集成测试...")

	cookiePath := "../cookies.json"
	store := cookies.NewFileCookieStore(cookiePath)

	// 启动浏览器，显示界面（如果是在无头环境下，go-rod 依然会执行）
	b := browser.NewBrowser(false)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	a := NewArticlePublishAction(page, store)

	log.Info("正在导航到发布页面...")
	if err := page.Navigate("https://mp.toutiao.com/profile_v4/graphic/publish"); err != nil {
		t.Fatalf("导航失败: %v", err)
	}
	if err := page.WaitLoad(); err != nil {
		t.Fatalf("等待加载失败: %v", err)
	}
	time.Sleep(3 * time.Second)

	// 检查是否被重定向到登录页
	info, _ := page.Info()
	if info != nil && (strings.Contains(info.URL, "login") || strings.Contains(info.URL, "auth")) {
		log.Warn("检测到当前未登录或 Cookie 已失效！")
		log.Warn("==================================================================")
		log.Warn("请在弹出的 Chrome 浏览器窗口中手动完成扫码登录或账号密码登录。")
		log.Warn("脚本将自动监测登录状态，登录成功后会自动保存 Cookie 并继续执行。")
		log.Warn("==================================================================")

		// 轮询等待登录成功
		loginTimeout := 300 * time.Second
		deadline := time.Now().Add(loginTimeout)
		loggedIn := false
		for time.Now().Before(deadline) {
			currentInfo, err := page.Info()
			if err == nil && IsLoginSuccessURL(currentInfo.URL) {
				log.Info("检测到登录成功！")
				loggedIn = true
				break
			}
			time.Sleep(2 * time.Second)
		}

		if !loggedIn {
			t.Fatalf("等待手动登录超时（5分钟），测试失败。")
		}

		// 登录成功，保存最新的 Cookie
		log.Info("正在保存最新的 Cookie 到 cookies.json...")
		browserCookies, err := page.Cookies(nil)
		if err != nil {
			t.Fatalf("获取浏览器 Cookie 失败: %v", err)
		}

		var entries []map[string]interface{}
		for _, c := range browserCookies {
			entry := map[string]interface{}{
				"name":     c.Name,
				"value":    c.Value,
				"domain":   c.Domain,
				"path":     c.Path,
				"expires":  int64(c.Expires),
				"httpOnly": c.HTTPOnly,
				"secure":   c.Secure,
			}
			entries = append(entries, entry)
		}

		// 序列化
		importJSON, err := json.Marshal(entries)
		if err != nil {
			t.Fatalf("序列化 Cookie 失败: %v", err)
		}
		if err := store.SaveCookies(importJSON); err != nil {
			t.Fatalf("保存 Cookie 失败: %v", err)
		}
		log.Info("Cookie 保存成功，正在重新导航回发文页面...")

		// 再次导航回发文页面
		if err := page.Navigate("https://mp.toutiao.com/profile_v4/graphic/publish"); err != nil {
			t.Fatalf("重新导航失败: %v", err)
		}
		if err := page.WaitLoad(); err != nil {
			t.Fatalf("重新导航等待加载失败: %v", err)
		}
		time.Sleep(3 * time.Second)
	}

	// 输入标题
	log.Info("正在输入测试标题...")
	if err := a.inputTitle("移动云智能新空间：开启AI算力与模型服务的新篇章"); err != nil {
		info, _ := page.Info()
		currentURL := ""
		if info != nil {
			currentURL = info.URL
		}
		_ = page.MustScreenshot("../scratch_title_error.png")
		t.Fatalf("输入标题失败 (当前URL: %s): %v. 截图已保存至 scratch_title_error.png", currentURL, err)
	}
	time.Sleep(1 * time.Second)

	// 输入内容（含本地生成的测试图片路径）
	log.Info("正在输入带插图的Markdown内容...")
	content := "随着人工智能与大语言模型的快速发展，算力调度与高效的模型服务已经成为企业智能转型的核心基础设施。\n\n" +
		"![智能算力中心](./testdata/test_cloud_computing.png)\n\n" +
		"移动云智能新空间不仅提供了强大的算力支撑，还通过一站式的模型服务工具，降低了开发者与企业应用AI的门槛。无论是模型微调还是快速部署，均能在这里得到高效解决。"

	if err := a.inputContent(content); err != nil {
		t.Fatalf("输入正文插图失败: %v", err)
	}
	time.Sleep(2 * time.Second)

	// 验证内容是否真正写入
	if err := a.verifyContent(); err != nil {
		t.Errorf("内容验证警告: %v", err)
	}

	// 截图保存以供人工检查
	screenshotPath := "../scratch_insert_result.png"
	_ = page.MustScreenshot(screenshotPath)
	log.Infof("已保存测试插图截图至: %s", screenshotPath)

	// 尝试点击“存草稿”按钮
	log.Info("正在尝试保存为草稿...")
	res, err := page.Eval(`() => {
		let btn = Array.from(document.querySelectorAll('button')).find(b => {
			let text = b.textContent ? b.textContent.trim() : '';
			return (text === '存草稿' || text === '保存草稿' || text === '保存') && b.offsetWidth > 0;
		});
		if (btn) {
			btn.click();
			return { success: true, text: btn.textContent.trim() };
		}
		return { success: false };
	}`)

	if err != nil {
		log.Warnf("点击存草稿JS执行出错: %v", err)
	} else if res != nil {
		log.Infof("存草稿结果: %v", res.Value.String())
	}

	time.Sleep(3 * time.Second)
	// 再次截图，确认是否保存成功
	_ = page.MustScreenshot("../scratch_save_draft_result.png")
	log.Info("测试完成")
}
