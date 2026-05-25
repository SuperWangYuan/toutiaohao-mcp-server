package toutiaohao

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-rod/rod"
	log "github.com/sirupsen/logrus"
)

// downloadImageToTemp 将图片（本地路径或 HTTP 网络 URL）规范化。
// 如果是网络图片，下载到本地临时文件；如果是本地相对路径，转换为绝对路径。
// 返回本地绝对路径、清理函数（在上传结束后调用）以及可能发生的错误。
func downloadImageToTemp(imgURL string) (string, func(), error) {
	// 1. 如果是本地路径，转换为绝对路径
	if !strings.HasPrefix(imgURL, "http://") && !strings.HasPrefix(imgURL, "https://") {
		absPath, err := filepath.Abs(imgURL)
		if err != nil {
			log.Warnf("获取本地图片绝对路径失败: %s, err: %v", imgURL, err)
			return imgURL, func() {}, nil
		}
		// 校验文件是否存在
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			return "", func() {}, fmt.Errorf("本地图片不存在: %s (绝对路径: %s)", imgURL, absPath)
		}
		return absPath, func() {}, nil
	}

	// 2. 如果是网络图片，发起 HTTP 下载
	log.Infof("检测到网络图片 URL，正在下载到临时目录: %s", imgURL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", imgURL, nil)
	if err != nil {
		return "", nil, fmt.Errorf("创建下载请求失败: %w", err)
	}

	// 模拟浏览器头部以防止某些图床防盗链拒绝下载
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("下载网络图片失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("下载图片失败，HTTP 状态码: %d", resp.StatusCode)
	}

	// 识别图片文件类型后缀
	ext := ".jpg"
	lowerURL := strings.ToLower(imgURL)
	if strings.Contains(lowerURL, ".png") {
		ext = ".png"
	} else if strings.Contains(lowerURL, ".gif") {
		ext = ".gif"
	} else if strings.Contains(lowerURL, ".webp") {
		ext = ".webp"
	} else if strings.Contains(lowerURL, ".jpeg") {
		ext = ".jpeg"
	}

	// 创建临时文件
	tempFile, err := os.CreateTemp("", "toutiaohao-download-img-*"+ext)
	if err != nil {
		return "", nil, fmt.Errorf("创建临时图片文件失败: %w", err)
	}
	defer tempFile.Close()

	_, err = io.Copy(tempFile, resp.Body)
	if err != nil {
		_ = os.Remove(tempFile.Name())
		return "", nil, fmt.Errorf("保存临时图片数据失败: %w", err)
	}

	tempPath := tempFile.Name()
	log.Infof("网络图片成功下载至本地临时路径: %s", tempPath)

	cleanup := func() {
		_ = os.Remove(tempPath)
		log.Infof("已清理网络图片临时文件: %s", tempPath)
	}

	return tempPath, cleanup, nil
}

func findElement(page *rod.Page, timeout time.Duration, selectors []string) (*rod.Element, string, error) {
	var lastErr error
	for _, sel := range selectors {
		var (
			el  *rod.Element
			err error
		)
		if isXPath(sel) {
			el, err = page.Timeout(timeout).ElementX(sel)
		} else {
			el, err = page.Timeout(timeout).Element(sel)
		}
		if err == nil && el != nil {
			return el.CancelTimeout(), sel, nil
		}
		lastErr = err
	}
	return nil, "", fmt.Errorf("no element found from selectors %v: %w", selectors, lastErr)
}

func isXPath(selector string) bool {
	trimmed := strings.TrimSpace(selector)
	return strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, ".//") || strings.HasPrefix(trimmed, "(")
}

func inputText(el *rod.Element, text string) error {
	tagName, _ := el.Eval(`() => this.tagName.toLowerCase()`)
	if tagName != nil {
		switch tagName.Value.Str() {
		case "input", "textarea":
			_ = el.SelectAllText()
			return el.Input(text)
		}
	}

	// 对于 contenteditable 元素（如 ProseMirror），需要特殊处理
	// ProseMirror 有自己的内部状态，直接改 innerHTML 不会更新其状态
	_, err := el.Timeout(10*time.Second).Eval(`text => {
		// 方式 1: 尝试通过 ProseMirror view API 直接操作编辑器状态
		// ProseMirror 的 view 实例通常挂在 DOM 元素的 pmViewDesc 属性上
		let view = null;
		if (this.pmViewDesc && this.pmViewDesc.view) {
			view = this.pmViewDesc.view;
		} else if (this.cmView && this.cmView.view) {
			view = this.cmView.view;
		}

		if (view && view.state && view.dispatch) {
			try {
				const { state } = view;
				// 创建一个替换整个文档内容的事务
				const lines = String(text).split(/\r?\n/);
				const nodes = [];
				const schema = state.schema;
				for (const line of lines) {
					if (line.trim() === '') {
						nodes.push(schema.nodes.paragraph.create());
					} else {
						nodes.push(schema.nodes.paragraph.create(null, schema.text(line)));
					}
				}
				const newDoc = schema.nodes.doc.create(null, nodes);
				const tr = state.tr.replaceWith(0, state.doc.content.size, newDoc.content);
				view.dispatch(tr);
				view.focus();
				return;
			} catch(e) {
				console.warn('ProseMirror API failed:', e);
			}
		}

		// 方式 2: 使用 clipboard 粘贴方式（能正确触发编辑器的 paste handler）
		this.focus();
		try {
			// 先选中所有内容
			const selection = window.getSelection();
			const range = document.createRange();
			range.selectNodeContents(this);
			selection.removeAllRanges();
			selection.addRange(range);

			// 构建 ClipboardEvent 进行粘贴
			const clipboardData = new DataTransfer();
			clipboardData.setData('text/plain', text);
			const pasteEvent = new ClipboardEvent('paste', {
				bubbles: true,
				cancelable: true,
				clipboardData: clipboardData
			});
			const handled = !this.dispatchEvent(pasteEvent);
			if (handled || this.innerText.trim().length > 0) {
				return;
			}
		} catch(e) {
			console.warn('Clipboard paste failed:', e);
		}

		// 方式 3: execCommand insertText（兼容老编辑器）
		this.focus();
		this.innerHTML = '';
		if (document.execCommand) {
			document.execCommand('selectAll', false, null);
			document.execCommand('insertText', false, text);
		}

		// 如果 execCommand 也没效果，最后用 innerHTML
		if (this.innerText.trim().length === 0) {
			const lines = String(text).split(/\r?\n/);
			for (const line of lines) {
				const p = document.createElement('p');
				p.textContent = line || '\u00A0';
				this.appendChild(p);
			}
		}

		// 派发事件通知框架
		const inputEvent = typeof InputEvent === 'function'
			? new InputEvent('input', {bubbles: true, inputType: 'insertText', data: text})
			: new Event('input', {bubbles: true});
		this.dispatchEvent(inputEvent);
		this.dispatchEvent(new Event('change', {bubbles: true}));
	}`, text)
	return err
}

func clickFirst(page *rod.Page, timeout time.Duration, selectors []string, description string) error {
	el, sel, err := findElement(page, timeout, selectors)
	if err != nil {
		return fmt.Errorf("%s not found: %w", description, err)
	}
	log.Infof("Found %s using selector: %s", description, sel)
	_, err = el.Timeout(10 * time.Second).Eval(`() => {` + SafeScrollJS + `
		scrollIntoViewSafe(this);
		this.click();
	}`)
	return err
}

// 发布成功后页面可能跳转到的 URL 片段
// 注意：不能包含过于宽泛的模式（如 mp.toutiao.com/profile），
// 否则会匹配到发布页自身 URL 导致误判
var publishSuccessURLFragments = []string{
	"profile_v4/index",
	"profile_v4/weitoutiao/manage",
	"profile_v4/graphic/manage",
	"profile_v4/graphic/article_list",
	"article/manage",
}

// 发布失败时页面可能出现的提示文本
var publishErrorTexts = []string{
	"发布失败",
	"内容违规",
	"请重试",
	"审核不通过",
	"操作失败",
	"标题不能为空",
	"正文不能为空",
	"内容不能为空",
}

// waitForPublishResult 等待并检测发布结果
// 在点击发布按钮后调用，通过以下方式判断发布是否成功：
// 1. 页面 URL 发生变化且跳转到管理页（成功）
// 2. 页面出现成功提示 toast（成功）
// 3. 页面出现错误提示文本（失败）
// 4. 超时仍停留在发布页（失败）
func waitForPublishResult(page *rod.Page, timeout time.Duration) error {
	// 记录点击发布前的 URL，用于对比是否发生跳转
	initialURL := ""
	if info, err := page.Info(); err == nil && info != nil {
		initialURL = info.URL
	}

	interval := 500 * time.Millisecond
	deadline := time.Now().Add(timeout)

	// 先等待 1 秒让发布请求有时间发出
	time.Sleep(1 * time.Second)

	for time.Now().Before(deadline) {
		info, err := page.Info()
		if err == nil && info != nil {
			currentURL := info.URL

			// 检查 URL 是否已跳转到成功页面
			for _, fragment := range publishSuccessURLFragments {
				if strings.Contains(currentURL, fragment) {
					log.Infof("发布成功，页面已跳转到: %s", currentURL)
					return nil
				}
			}

			// URL 变化了但不在成功列表中，可能是其他跳转
			if currentURL != initialURL && initialURL != "" {
				log.Infof("页面 URL 已变化: %s -> %s", initialURL, currentURL)
				// 如果跳转到了非发布页，大概率是成功了
				if !strings.Contains(currentURL, "publish") {
					log.Info("发布后页面离开了发布页，判断为发布成功")
					return nil
				}
			}
		}

		// 检查是否出现成功提示（如 toast）
		successTexts := []string{"发布成功", "已发布", "发表成功"}
		for _, sText := range successTexts {
			sel := fmt.Sprintf(`//*[contains(@class, 'toast') or contains(@class, 'message') or contains(@class, 'notification') or contains(@class, 'alert') or contains(@class, 'semi-') or contains(@class, 'byte-') or @role='alert']//*[contains(text(), '%s')] | //*[contains(@class, 'toast') or contains(@class, 'message') or contains(@class, 'notification') or contains(@class, 'alert') or contains(@class, 'semi-') or contains(@class, 'byte-') or @role='alert'][contains(text(), '%s')]`, sText, sText)
			el, err := page.Timeout(200 * time.Millisecond).ElementX(sel)
			if err == nil && el != nil {
				log.Infof("检测到发布成功提示: %s", sText)
				return nil
			}
		}

		// 检查页面是否出现错误提示
		for _, errText := range publishErrorTexts {
			sel := fmt.Sprintf(`//*[contains(text(), '%s')]`, errText)
			el, err := page.Timeout(200 * time.Millisecond).ElementX(sel)
			if err == nil && el != nil {
				visibleText, _ := el.Text()
				safeScreenshot(page, "./screenshot_err.png")
				log.Warnf("发布失败，页面有错误提示，已保存截图到 screenshot_err.png")
				return fmt.Errorf("发布失败，页面提示: %s", visibleText)
			}
		}

		time.Sleep(interval)
	}

	// 超时——发布很可能没有成功
	safeScreenshot(page, "./screenshot_timeout.png")
	log.Warn("发布检测超时，已保存截图到 screenshot_timeout.png")
	return fmt.Errorf("发布结果检测超时（%v），页面仍停留在发布页，未检测到成功跳转或提示", timeout)
}

// inputTextWithFallback 带 fallback 的文本输入
// 先尝试 ProseMirror 方式（通过 innerHTML + 事件派发），
// 如果失败则回退到 Rod 原生键盘输入
func inputTextWithFallback(el *rod.Element, text string) error {
	// 先尝试 inputText 的主方式
	err := inputText(el, text)
	if err == nil {
		// 验证内容是否真的写入了
		content, evalErr := el.Eval(`() => this.innerText || this.value || ''`)
		if evalErr == nil && content != nil && strings.TrimSpace(content.Value.Str()) != "" {
			return nil
		}
		log.Warn("主方式输入后内容为空，尝试 fallback 方式")
	} else {
		log.Warnf("主方式输入失败: %v，尝试 fallback 方式", err)
	}

	// Fallback: 使用 Rod 原生的 MustSelectAllText + Input
	tagName, _ := el.Eval(`() => this.tagName.toLowerCase()`)
	if tagName != nil {
		switch tagName.Value.Str() {
		case "input", "textarea":
			_ = el.SelectAllText()
			return el.Input(text)
		}
	}

	// 对于 contenteditable 元素，尝试另一种事件驱动方式
	_, err = el.Eval(`text => {
		this.focus();
		// 尝试使用 document.execCommand（兼容较老的编辑器框架）
		this.innerHTML = '';
		if (document.execCommand) {
			document.execCommand('insertText', false, text);
		} else {
			this.textContent = text;
		}
		this.dispatchEvent(new Event('input', {bubbles: true}));
		this.dispatchEvent(new Event('change', {bubbles: true}));
	}`, text)
	return err
}

// SafeScrollJS 用于将元素安全滚动到视口中并防止被固定的 header（如顶栏）遮挡的 JavaScript 辅助函数。
const SafeScrollJS = `
function scrollIntoViewSafe(el) {
	if (!el) return;
	// 临时禁用平滑滚动，确保滚动和坐标获取是同步的
	const scrollableElements = [];
	let p = el;
	while (p) {
		if (p.style && p.style.scrollBehavior === 'smooth') {
			scrollableElements.push({ el: p, prev: 'smooth' });
			p.style.scrollBehavior = 'auto';
		}
		p = p.parentElement;
	}
	if (document.documentElement && document.documentElement.style && document.documentElement.style.scrollBehavior === 'smooth') {
		scrollableElements.push({ el: document.documentElement, prev: 'smooth' });
		document.documentElement.style.scrollBehavior = 'auto';
	}
	if (document.body && document.body.style && document.body.style.scrollBehavior === 'smooth') {
		scrollableElements.push({ el: document.body, prev: 'smooth' });
		document.body.style.scrollBehavior = 'auto';
	}

	const targetTop = 250; // 目标高度，距离视口顶部 250px，既避开了顶部固定栏又不会太靠下
	let rect = el.getBoundingClientRect();
	let diff = rect.top - targetTop; // 正数表示偏下（需要向上滚），负数表示偏上（需要向下滚）

	if (Math.abs(diff) >= 1) {
		// 从内向外寻找可滚动容器，并调整其 scrollTop
		let parent = el.parentElement;
		while (parent && parent !== document.documentElement && parent !== document.body) {
			const style = window.getComputedStyle(parent);
			const overflowY = style.overflowY;
			const isScrollable = (overflowY === 'auto' || overflowY === 'scroll') && parent.scrollHeight > parent.clientHeight;
			if (isScrollable) {
				if (diff > 0) {
					let maxScroll = parent.scrollHeight - parent.clientHeight - parent.scrollTop;
					let scrollAmt = Math.min(maxScroll, diff);
					parent.scrollTop += scrollAmt;
					diff -= scrollAmt;
				} else {
					let scrollAmt = Math.min(parent.scrollTop, -diff);
					parent.scrollTop -= scrollAmt;
					diff += scrollAmt;
				}
				if (Math.abs(diff) < 1) break;
			}
			parent = parent.parentElement;
		}

		// 如果仍有剩余位移，则滚动 window
		if (Math.abs(diff) >= 1) {
			window.scrollBy(0, diff);
		}
	}

	// 恢复平滑滚动设置
	scrollableElements.forEach(item => {
		if (item.el && item.el.style) {
			item.el.style.scrollBehavior = item.prev;
		}
	});
}
`

// parsePublishTime 解析并标准化定时发布时间，返回 "YYYY-MM-DD HH:mm" 的字符串格式。
func parsePublishTime(publishTime interface{}) (string, error) {
	if publishTime == nil {
		return "", nil
	}

	var targetTime time.Time

	switch v := publishTime.(type) {
	case string:
		val := strings.TrimSpace(v)
		if val == "" {
			return "", nil
		}
		// 尝试解析各种常见时间格式
		formats := []string{
			"2006-01-02 15:04:05",
			"2006-01-02 15:04",
			"2006/01/02 15:04:05",
			"2006/01/02 15:04",
			time.RFC3339,
		}
		var parsed bool
		for _, f := range formats {
			if t, err := time.ParseInLocation(f, val, time.Local); err == nil {
				targetTime = t
				parsed = true
				break
			}
		}
		if !parsed {
			return "", fmt.Errorf("无法解析的发布时间字符串: %s. 支持的格式: YYYY-MM-DD HH:mm:ss 或 YYYY-MM-DD HH:mm", val)
		}

	case int:
		targetTime = time.Unix(int64(v), 0)
	case int64:
		targetTime = time.Unix(v, 0)
	case float64:
		targetTime = time.Unix(int64(v), 0)
	default:
		return "", fmt.Errorf("不支持的发布时间类型: %T. 仅支持 Unix 时间戳或格式化时间字符串", publishTime)
	}

	// 打印警告以辅助调试，但不强行中断发文
	now := time.Now()
	minTime := now.Add(2 * time.Hour)
	maxTime := now.Add(30 * 24 * time.Hour)
	if targetTime.Before(minTime) || targetTime.After(maxTime) {
		log.Warnf("【时间提示】定时发布时间 %s 可能不符合头条平台要求。头条通常要求在“当前时间+2小时 到 30天内”之间。当前时间：%s", 
			targetTime.Format("2006-01-02 15:04"), now.Format("2006-01-02 15:04"))
	}

	return targetTime.Format("2006-01-02 15:04"), nil
}

// setPublishTime 浏览器自动化设置定时发布时间
func setPublishTime(page *rod.Page, publishTime interface{}) error {
	timeStr, err := parsePublishTime(publishTime)
	if err != nil {
		return err
	}
	if timeStr == "" {
		return nil
	}

	log.Infof("检测到定时发布选项，目标时间: %s", timeStr)

	// 1. 定位并点击“定时发布”单选标签
	scheduleRadioSelectors := []string{
		`//span[contains(text(), '定时发布')]`,
		`//label[contains(., '定时发布')]`,
		`//span[contains(text(), '定时')]/preceding-sibling::span/input[@type='radio']`,
		`[class*='radio'] input[value='1']`, // 有时头条定时发布单选框 value 为 1
	}

	log.Info("正在勾选“定时发布”单选按钮...")
	elRadio, selRadio, err := findElement(page, 5*time.Second, scheduleRadioSelectors)
	if err != nil {
		safeScreenshot(page, "./screenshot_publish_time_radio_error.png")
		htmlVal, _ := page.Eval(`() => document.body.innerHTML`)
		if htmlVal != nil {
			_ = os.WriteFile("./dom_dump_radio.html", []byte(htmlVal.Value.Str()), 0644)
			log.Warn("已保存 DOM Dump 至 ./dom_dump_radio.html")
		}
		return fmt.Errorf("未找到“定时发布”按钮/选项: %w (已保存调试截图至 screenshot_publish_time_radio_error.png 并保存 DOM Dump 至 ./dom_dump_radio.html)", err)
	}
	log.Infof("已找到定时发布按钮，使用选择器: %s，正在执行 JS 点击...", selRadio)

	// 滚动并触发 JS 点击
	_, _ = elRadio.Eval(`() => {` + SafeScrollJS + `
		scrollIntoViewSafe(this);
		this.click();
	}`)
	time.Sleep(2000 * time.Millisecond) // 等待时间选择弹窗渲染出来

	// 2. 定位日期时间输入框并填入时间
	timeInputSelectors := []string{
		`input[placeholder*='选择日期']`,
		`input[placeholder*='请选择']`,
		`input[class*='datepicker']`,
		`.byte-datepicker input`,
		`.semi-datepicker input`,
	}

	log.Info("正在定位日期时间输入框...")
	elInput, selInput, err := findElement(page, 5*time.Second, timeInputSelectors)
	if err != nil {
		safeScreenshot(page, "./screenshot_publish_time_error.png")
		res, _ := page.Eval(`() => {
			let inputs = Array.from(document.querySelectorAll('input')).map(el => ({
				type: el.type,
				placeholder: el.placeholder,
				className: el.className,
				value: el.value,
				visible: el.offsetWidth > 0
			}));
			
			let schedTextEls = Array.from(document.querySelectorAll('*')).filter(el => {
				let childNodes = Array.from(el.childNodes);
				let directText = childNodes.filter(n => n.nodeType === Node.TEXT_NODE).map(n => n.textContent.trim()).join('');
				return (directText === '定时发布' || directText === '立即发布') && el.offsetWidth > 0;
			}).map(el => {
				let path = [];
				let curr = el;
				for (let i = 0; i < 4; i++) {
					if (curr) {
						path.push({
							tagName: curr.tagName,
							className: curr.className,
							outerHTML: curr.outerHTML.substring(0, 250)
						});
						curr = curr.parentElement;
					}
				}
				return path;
			});
			
			let modals = Array.from(document.querySelectorAll('.semi-modal, .byte-modal, [class*="modal"], [class*="dialog"], [class*="popover"], [class*="datepicker"], [class*="popup"]')).filter(el => el.offsetWidth > 0).map(el => ({
				className: el.className,
				tagName: el.tagName,
				outerHTML: el.outerHTML.substring(0, 1000)
			}));

			let dateRelated = Array.from(document.querySelectorAll('*')).filter(el => {
				let text = el.innerText ? el.innerText.trim() : '';
				let hasDateText = text.includes('选择') || text.includes('日期') || text.includes('时间') || text.includes('请选择');
				return hasDateText && el.offsetWidth > 0 && el.offsetHeight > 0 && el.children.length < 5;
			}).map(el => ({
				tagName: el.tagName,
				className: el.className,
				text: el.innerText.substring(0, 60),
				outerHTML: el.outerHTML.substring(0, 200)
			}));
			
			let redErrors = Array.from(document.querySelectorAll('*')).filter(el => {
				let style = window.getComputedStyle(el);
				let isRed = style.color === 'rgb(240, 65, 66)' || style.color === 'red' || (el.className && typeof el.className === 'string' && el.className.includes('error'));
				return isRed && el.offsetWidth > 0 && el.offsetHeight > 0 && el.innerText.trim().length > 0 && el.children.length === 0;
			}).map(el => el.innerText.trim());
			
			return { inputs, schedTextEls, modals, dateRelated, redErrors };
		}`)
		if res != nil {
			log.Warnf("调试信息: %s", res.Value.String())
		}
		htmlVal, _ := page.Eval(`() => document.body.innerHTML`)
		if htmlVal != nil {
			_ = os.WriteFile("./dom_dump.html", []byte(htmlVal.Value.Str()), 0644)
			log.Warn("已保存 DOM Dump 至 ./dom_dump.html")
		}
		return fmt.Errorf("未找到日期时间输入框: %w (已保存调试截图至 screenshot_publish_time_error.png 并保存 DOM Dump 至 ./dom_dump.html)", err)
	}
	log.Infof("已找到日期时间输入框，选择器: %s", selInput)

	// 先清空并输入时间
	_ = elInput.SelectAllText()
	if err := elInput.Input(timeStr); err != nil {
		return fmt.Errorf("向日期时间输入框输入时间失败: %w", err)
	}
	time.Sleep(500 * time.Millisecond)

	// 3. 点击时间面板上的“确定”按钮以关闭面板并使输入生效
	log.Info("正在确认日期时间选择面板...")
	
	// 首先尝试在输入框里敲击 Enter 键
	_, _ = elInput.Eval(`() => {
		this.focus();
		const enterEvent = new KeyboardEvent('keydown', {
			key: 'Enter',
			code: 'Enter',
			keyCode: 13,
			which: 13,
			bubbles: true
		});
		this.dispatchEvent(enterEvent);
		const changeEvent = new Event('change', { bubbles: true });
		this.dispatchEvent(changeEvent);
	}`)
	time.Sleep(500 * time.Millisecond)

	// 另外在 DOM 查找确定按钮并点击
	confirmSelectors := []string{
		`//button[contains(text(), '确定')]`,
		`//span[contains(text(), '确定')]/ancestor::button`,
		`.byte-datepicker-btn-confirm`,
		`button[class*='confirm']`,
	}

	if elConfirm, _, errConfirm := findElement(page, 2*time.Second, confirmSelectors); errConfirm == nil && elConfirm != nil {
		_, _ = elConfirm.Eval(`() => this.click()`)
		time.Sleep(1000 * time.Millisecond)
	}

	// 校验时间是否填入成功
	val, errVal := elInput.Eval(`() => this.value`)
	if errVal == nil && val != nil {
		log.Infof("日期时间设置成功，当前输入框内值: %s", val.Value.Str())
	}

	return nil
}

// safeScreenshot 安全截图，捕获可能因浏览器关闭/断连导致的 panic，不影响后续错误返回
func safeScreenshot(page *rod.Page, path string) {
	defer func() {
		if r := recover(); r != nil {
			log.Warnf("截图失败并捕获异常 (通常是浏览器连接已断开): %v", r)
		}
	}()
	_ = page.MustScreenshot(path)
}



