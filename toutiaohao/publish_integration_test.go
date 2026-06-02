package toutiaohao

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	// 优先复制已有的真实合规图片
	copied := false
	possibleImages := []string{"../scratch_insert_result.png", "../scratch_save_draft_result.png", "../screenshot_upload_error.png", "../publish_after_first_click.png"}
	for _, pImg := range possibleImages {
		if _, err := os.Stat(pImg); err == nil {
			input, errRead := os.ReadFile(pImg)
			if errRead == nil {
				errWrite := os.WriteFile(testImagePath, input, 0644)
				if errWrite == nil {
					copied = true
					log.Infof("成功从 %s 复制真实合规图片用于上传测试", pImg)
					break
				}
			}
		}
	}

	if !copied {
		log.Warn("无法复制真实图片，回退到写入 Dummy 图片数据")
		dummyPNG := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15c4\x00\x00\x00\rIDATx\x9cc`\x00\x00\x00\x02\x00\x01H\xaf\xa4q\x00\x00\x00\x00IEND\xaeB`\x82")
		if err := os.WriteFile(testImagePath, dummyPNG, 0644); err != nil {
			t.Fatalf("创建临时测试图片失败: %v", err)
		}
	}

	var articleID string
	// 设置环境变量以便内部 browser 加载根目录下的 cookies.json
	os.Setenv("TOUTIAOHAO_COOKIES_PATH", "../cookies.json")

	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})
	log.Info("开始图文插图集成测试...")

	cookiePath := "../cookies.json"
	store := cookies.NewFileCookieStore(cookiePath)

	// 启动浏览器，显示界面（如果是在无头环境下，go-rod 依然会执行）
	b := browser.NewBrowser(true)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	// 测试结束时自动删除本地图片和线上临时草稿
	defer func() {
		_ = os.RemoveAll(testdataDir)
		if articleID != "" {
			log.Infof("测试结束，正在自动清理（删除）临时草稿文章 %s ...", articleID)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if delErr := DeleteDraftByBrowser(ctx, page, articleID); delErr != nil {
				log.Warnf("清理临时草稿文章失败: %v", delErr)
			} else {
				log.Info("临时测试草稿文章已通过浏览器物理删除成功。")
			}
		}
	}()

	a := NewArticlePublishAction(page, store)

	log.Info("正在导航到发布页面...")
	if err := page.Navigate("https://mp.toutiao.com/profile_v4/graphic/publish"); err != nil {
		t.Fatalf("导航失败: %v", err)
	}
	if err := page.WaitLoad(); err != nil {
		t.Fatalf("等待加载失败: %v", err)
	}
	time.Sleep(3 * time.Second)

	// 检查并处理登录状态 (包含扫码等待)
	if err := EnsureLogin(page, store); err != nil {
		t.Fatalf("登录校验/扫码失败: %v", err)
	}

	// 重新导航回发文页面以确保正确渲染
	log.Info("重新导航至发文页面...")
	if err := page.Navigate("https://mp.toutiao.com/profile_v4/graphic/publish"); err != nil {
		t.Fatalf("重新导航失败: %v", err)
	}
	if err := page.WaitLoad(); err != nil {
		t.Fatalf("重新导航等待加载失败: %v", err)
	}
	time.Sleep(3 * time.Second)

	// 输入标题（每次测试加上时间戳使其唯一）
	uniqueTitle := fmt.Sprintf("测试移动云发布%d", time.Now().Unix()%100000)
	log.Infof("正在输入测试标题: %s", uniqueTitle)
	if err := a.inputTitle(uniqueTitle); err != nil {
		info, _ := page.Info()
		currentURL := ""
		if info != nil {
			currentURL = info.URL
		}
		_ = page.MustScreenshot("../scratch_title_error.png")
		t.Fatalf("输入标题失败 (当前URL: %s): %v. 截图已保存至 scratch_title_error.png", currentURL, err)
	}
	time.Sleep(1 * time.Second)

	// 输入内容（只使用纯文本以规避 Dummy 图片二进制上传校验失败导致发布按钮锁死的问题）
	log.Info("正在输入纯文本内容...")
	content := "随着人工智能与大语言模型的快速发展，算力调度与高效的模型服务已经成为企业智能转型的核心基础设施。\n\n" +
		"移动云智能新空间不仅提供了强大的算力支撑，还通过一站式的模型服务工具，降低了开发者与企业应用AI的门槛。无论是模型微调还是快速部署，均能在这里得到高效解决。"

	if err := a.inputContent(content, nil); err != nil {
		t.Fatalf("输入正文失败: %v", err)
	}
	time.Sleep(2 * time.Second)

	// 显式设置封面模式为单图，测试封面上传
	log.Info("正在设置封面模式为“单图”...")
	if err := a.setCoverMode("单图"); err != nil {
		t.Fatalf("设置封面模式失败: %v", err)
	}
	time.Sleep(1 * time.Second)

	log.Info("正在上传测试封面图片...")
	if err := a.uploadCovers([]string{testImagePath}, false); err != nil {
		t.Fatalf("上传封面失败: %v", err)
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

	// 尝试设置原创/首发选项
	log.Info("正在测试设置原创/首发标记...")
	a.setOriginal()
	time.Sleep(2 * time.Second)

	// 尝试点击底部的“定时发布”按钮以弹出时间设置弹窗
	log.Info("正在点击底部的定时发布大按钮以弹出时间选择...")
	btnPublish, _, err := findElement(page, 5*time.Second, []string{
		`//button[span[contains(text(), '定时发布')]]`,
		`//button[contains(., '定时发布')]`,
	})
	if err != nil {
		t.Fatalf("未找到底部的定时发布按钮: %v", err)
	}
	log.Info("找到定时发布按钮，正在执行 JS 点击...")
	_, _ = btnPublish.Eval(`() => {` + SafeScrollJS + `
		scrollIntoViewSafe(this);
		this.click();
	}`)
	time.Sleep(3 * time.Second)

	// 尝试设置定时发布时间
	log.Info("正在测试设置定时发布时间...")
	testPublishTime := time.Now().Add(3 * time.Hour).Format("2006-01-02 15:04")
	if err := setPublishTime(page, testPublishTime); err != nil {
		resScan, errScan := page.Eval(`() => {
			let text = document.body.innerText || '';
			return text.includes('扫码') || text.includes('今日头条App') || text.includes('仅支持预览');
		}`)
		if errScan == nil && resScan != nil && resScan.Value.Bool() {
			log.Warn("【风控提示】当前发文账号在网页端被平台风控要求进行App扫码预览/发布，按钮点击动作已被成功触发，但时间选择器被扫码弹窗拦截。已判定点击触发逻辑正常。")
			t.Skip("账号网页端被风控拦截要求扫码，跳过后续设置时间流程")
		} else {
			t.Fatalf("设置定时发布时间失败: %v", err)
		}
	}
	time.Sleep(2 * time.Second)

	// 等待编辑器保存草稿
	log.Info("等待编辑器自动保存草稿以获取文章 ID...")
	time.Sleep(5 * time.Second)

	// 获取文章列表以提取新建文章的 ID，供 defer 清理
	log.Info("从列表中寻找新建的临时文章以获取 ID 用于清理...")
	ctxList, cancelList := context.WithTimeout(context.Background(), 1*time.Minute)
	params := &ArticleListParams{
		Status:   "all",
		Page:     1,
		PageSize: 5,
	}
	resp, errList := GetArticleList(ctxList, params, store)
	cancelList()
	if errList == nil && resp != nil {
		for _, art := range resp.Articles {
			if art.Title == uniqueTitle {
				articleID = art.ArticleID
				log.Infof("成功捕获新建草稿文章 ID: %s，将在测试退出时自动清理", articleID)
				break
			}
		}
	} else {
		log.Warnf("获取文章列表以提取 ID 失败（不影响发文测试结果，但需要手动清理草稿 %s）: %v", uniqueTitle, errList)
	}

	// 再次截图，确认是否保存成功
	_ = page.MustScreenshot("../scratch_save_draft_result.png")
	log.Info("测试完成")
}

func TestPublishArticleBodyImageManual(t *testing.T) {
	testdataDir := "./testdata_body_image"
	testImagePath := filepath.Join(testdataDir, "test_body_image.png")
	if err := os.MkdirAll(testdataDir, 0755); err != nil {
		t.Fatalf("创建临时测试目录失败: %v", err)
	}

	copied := false
	for _, pImg := range []string{"../scratch_insert_result.png", "../scratch_save_draft_result.png", "../publish_after_first_click.png"} {
		if input, errRead := os.ReadFile(pImg); errRead == nil {
			if errWrite := os.WriteFile(testImagePath, input, 0644); errWrite == nil {
				copied = true
				log.Infof("成功从 %s 复制真实图片用于正文插图测试", pImg)
				break
			}
		}
	}
	if !copied {
		dummyPNG := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15c4\x00\x00\x00\rIDATx\x9cc`\x00\x00\x00\x02\x00\x01H\xaf\xa4q\x00\x00\x00\x00IEND\xaeB`\x82")
		if err := os.WriteFile(testImagePath, dummyPNG, 0644); err != nil {
			t.Fatalf("创建临时测试图片失败: %v", err)
		}
	}

	var articleID string
	os.Setenv("TOUTIAOHAO_COOKIES_PATH", "../cookies.json")
	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})

	cookiePath := "../cookies.json"
	store := cookies.NewFileCookieStore(cookiePath)
	b := browser.NewBrowser(true)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()
	defer func() {
		_ = os.RemoveAll(testdataDir)
		if articleID != "" {
			log.Infof("测试结束，正在自动清理正文插图临时草稿 %s ...", articleID)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if delErr := DeleteDraftByBrowser(ctx, page, articleID); delErr != nil {
				log.Warnf("清理正文插图临时草稿失败: %v", delErr)
			}
		}
	}()

	a := NewArticlePublishAction(page, store)
	if err := page.Navigate("https://mp.toutiao.com/profile_v4/graphic/publish"); err != nil {
		t.Fatalf("导航失败: %v", err)
	}
	if err := page.WaitLoad(); err != nil {
		t.Fatalf("等待加载失败: %v", err)
	}
	time.Sleep(3 * time.Second)

	if err := EnsureLogin(page, store); err != nil {
		t.Fatalf("登录校验/扫码失败: %v", err)
	}
	if err := page.Navigate("https://mp.toutiao.com/profile_v4/graphic/publish"); err != nil {
		t.Fatalf("重新导航失败: %v", err)
	}
	if err := page.WaitLoad(); err != nil {
		t.Fatalf("重新导航等待加载失败: %v", err)
	}
	time.Sleep(3 * time.Second)

	uniqueTitle := fmt.Sprintf("正文插图测试%d", time.Now().Unix()%100000)
	if err := a.inputTitle(uniqueTitle); err != nil {
		t.Fatalf("输入标题失败: %v", err)
	}

	content := fmt.Sprintf("正文插图上传测试开始。\n\n![正文测试图](%s)\n\n正文插图上传测试结束。", testImagePath)
	if err := a.inputContent(content, nil); err != nil {
		t.Fatalf("输入正文图片失败: %v", err)
	}
	time.Sleep(3 * time.Second)

	_ = page.MustScreenshot("../scratch_body_image_result.png")
	ctxList, cancelList := context.WithTimeout(context.Background(), 1*time.Minute)
	resp, errList := GetArticleList(ctxList, &ArticleListParams{Status: "all", Page: 1, PageSize: 5}, store)
	cancelList()
	if errList == nil && resp != nil {
		for _, art := range resp.Articles {
			if art.Title == uniqueTitle {
				articleID = art.ArticleID
				break
			}
		}
	}
}

func TestPublishMicroManual(t *testing.T) {
	// 动态创建测试插图目录与 dummy 临时图片文件
	testdataDir := "./testdata_micro"
	testImagePath := filepath.Join(testdataDir, "test_micro_image.png")
	if err := os.MkdirAll(testdataDir, 0755); err != nil {
		t.Fatalf("创建临时测试目录失败: %v", err)
	}
	// 优先复制已有的真实合规图片
	copied := false
	possibleImages := []string{"../scratch_insert_result.png", "../scratch_save_draft_result.png", "../screenshot_upload_error.png", "../publish_after_first_click.png"}
	for _, pImg := range possibleImages {
		if _, err := os.Stat(pImg); err == nil {
			input, errRead := os.ReadFile(pImg)
			if errRead == nil {
				errWrite := os.WriteFile(testImagePath, input, 0644)
				if errWrite == nil {
					copied = true
					log.Infof("成功从 %s 复制真实合规图片用于上传测试", pImg)
					break
				}
			}
		}
	}

	if !copied {
		log.Warn("无法复制真实图片，回退到写入 Dummy 图片数据")
		dummyPNG := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15c4\x00\x00\x00\rIDATx\x9cc`\x00\x00\x00\x02\x00\x01H\xaf\xa4q\x00\x00\x00\x00IEND\xaeB`\x82")
		if err := os.WriteFile(testImagePath, dummyPNG, 0644); err != nil {
			t.Fatalf("创建临时测试图片失败: %v", err)
		}
	}
	// 提供一个网络图片 URL
	webImageURL := "https://www.baidu.com/img/flexible/logo/pc/index.png"

	defer func() {
		_ = os.RemoveAll(testdataDir)
	}()

	os.Setenv("TOUTIAOHAO_COOKIES_PATH", "../cookies.json")

	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})
	log.Info("开始微头条配图集成测试...")

	cookiePath := "../cookies.json"
	store := cookies.NewFileCookieStore(cookiePath)

	b := browser.NewBrowser(true)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	a := NewMicroPostAction(page, store)

	log.Info("正在导航到微头条发布页面...")
	if err := page.Navigate("https://mp.toutiao.com/profile_v4/weitoutiao/publish?from=toutiao_pc"); err != nil {
		t.Fatalf("导航失败: %v", err)
	}
	if err := page.WaitLoad(); err != nil {
		t.Fatalf("等待加载失败: %v", err)
	}
	time.Sleep(3 * time.Second)

	// 检查并处理登录状态 (包含扫码等待)
	if err := EnsureLogin(page, store); err != nil {
		t.Fatalf("登录校验/扫码失败: %v", err)
	}

	// 重新导航回微头条页面确保处于登录后的正常状态
	log.Info("重新导航至微头条页面...")
	if err := page.Navigate("https://mp.toutiao.com/profile_v4/weitoutiao/publish?from=toutiao_pc"); err != nil {
		t.Fatalf("重新导航失败: %v", err)
	}
	if err := page.WaitLoad(); err != nil {
		t.Fatalf("重新导航等待加载失败: %v", err)
	}
	time.Sleep(3 * time.Second)

	// 输入正文
	log.Info("正在输入测试正文...")
	content := "这是一条微头条配图测试内容。包含一张本地动态生成的图片和一张从网络下载的百度 Logo 图片，旨在全面验证本地与网络图片在微头条发布流下的上传功能。"
	if err := a.inputContent(content); err != nil {
		t.Fatalf("输入正文失败: %v", err)
	}
	time.Sleep(1 * time.Second)

	// 上传图片（包含本地路径和 HTTP URL）
	log.Info("正在上传配图（本地与网络）...")
	images := []string{testImagePath, webImageURL}
	if err := a.uploadImages(images); err != nil {
		t.Fatalf("上传配图失败: %v", err)
	}
	time.Sleep(3 * time.Second)

	time.Sleep(1 * time.Second)

	// 截图保存以供人工检查
	screenshotPath := "../scratch_micro_publish_result.png"
	_ = page.MustScreenshot(screenshotPath)
	log.Infof("已保存微头条测试截图至: %s", screenshotPath)

	log.Info("微头条配图发布测试完成")
}

func TestUpdateArticleManual(t *testing.T) {
	os.Setenv("TOUTIAOHAO_COOKIES_PATH", "../cookies.json")
	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})
	log.Info("开始修改/更新文章集成测试（新建临时文章 -> 修改 -> 清理）...")

	cookiePath := "../cookies.json"
	store := cookies.NewFileCookieStore(cookiePath)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// 1. 启动浏览器
	b := browser.NewBrowser(true)
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	a := NewArticlePublishAction(page, store)

	// 2. 先导航到发布页并检查登录
	log.Info("正在导航到发布页并确保登录...")
	if err := page.Navigate("https://mp.toutiao.com/profile_v4/graphic/publish"); err != nil {
		t.Fatalf("导航失败: %v", err)
	}
	_ = page.WaitLoad()
	if err := EnsureLogin(page, store); err != nil {
		t.Fatalf("登录校验失败: %v", err)
	}

	// 3. 发布一篇临时新文章，标题唯一且在 30 字内
	uniqueTitle := fmt.Sprintf("临时新建测试文章%d", time.Now().Unix()%100000)
	log.Infof("准备发布临时新文章，标题: %s", uniqueTitle)

	// 输入标题和正文
	if err := a.inputTitle(uniqueTitle); err != nil {
		t.Fatalf("输入临时标题失败: %v", err)
	}
	time.Sleep(1 * time.Second)

	tempContent := "这是一篇临时创建的用于修改功能测试的文章。\n\n我们将在此文章发布后，通过接口对它执行二次修改，以验证修改接口的高容错性。"
	if err := a.inputContent(tempContent, nil); err != nil {
		t.Fatalf("输入临时正文失败: %v", err)
	}
	time.Sleep(1 * time.Second)

	// 设为无封面以防干扰发布
	if err := a.setCoverMode("无封面"); err != nil {
		t.Fatalf("设置无封面失败: %v", err)
	}

	// 保存为草稿
	log.Info("等待草稿自动保存并退出...")
	time.Sleep(8 * time.Second)

	// 4. 从文章列表拉取，寻找刚生成的草稿以获取其 pgc_id
	log.Info("获取文章列表以提取新建草稿的 ID...")
	params := &ArticleListParams{
		Status:   "all",
		Page:     1,
		PageSize: 5,
	}
	resp, err := GetArticleList(ctx, params, store)
	if err != nil {
		t.Fatalf("获取文章列表失败: %v", err)
	}

	var articleID string
	for _, art := range resp.Articles {
		if art.Title == uniqueTitle {
			articleID = art.ArticleID
			break
		}
	}
	if articleID == "" {
		t.Fatalf("未能从文章列表中提取到刚生成的临时草稿 ID (标题: %s)", uniqueTitle)
	}
	log.Infof("成功获取新建临时草稿 ID: %s", articleID)

	// 测试结束时，自动清理（删除）这篇临时草稿！
	defer func() {
		log.Infof("测试结束，正在自动清理（删除）临时草稿 %s ...", articleID)
		if delErr := DeleteDraftByBrowser(ctx, page, articleID); delErr != nil {
			log.Warnf("清理临时草稿失败: %v", delErr)
		} else {
			log.Info("临时测试草稿已物理删除成功，无测试垃圾残留。")
		}
	}()

	// 5. 对这篇临时草稿执行修改测试
	newTitle := fmt.Sprintf("修改测试iOS27倒计时%d", time.Now().Unix()%10000)
	newContent := "【修改测试成功】这是一次对刚刚临时新建草稿的编辑修改测试，修改后的标题和这一段内容已成功生效！"

	log.Infof("准备对草稿 %s 开展修改测试，新标题: %s", articleID, newTitle)

	// 调用 a.Update，同样保存为草稿，不发布
	if err := a.Update(ctx, articleID, newTitle, newContent, &ArticleOptions{SaveAsDraft: true}); err != nil {
		t.Fatalf("修改临时草稿失败: %v", err)
	}

	log.Info("修改/更新文章集成测试（全草稿沙盒）圆满成功！")
}
