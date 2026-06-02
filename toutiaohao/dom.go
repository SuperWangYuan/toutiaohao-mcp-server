package toutiaohao

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	log "github.com/sirupsen/logrus"
)

const NetworkTrackerJS = `(() => {
	if (window.__netTrackerInjected) return;
	window.__netTrackerInjected = true;
	window.__netLogs = [];

	const originalFetch = window.fetch;
	window.fetch = async function(...args) {
		const url = args[0];
		const options = args[1] || {};
		const logItem = { type: 'fetch', method: options.method || 'GET', url: typeof url === 'string' ? url : (url.url || ''), reqBody: options.body || '' };
		window.__netLogs.push(logItem);
		try {
			const response = await originalFetch.apply(this, args);
			const clone = response.clone();
			logItem.status = response.status;
			try {
				let text = await clone.text();
				logItem.resBody = text.substring(0, 500);
			} catch(e) {}
			return response;
		} catch(err) {
			logItem.error = err.message;
			throw err;
		}
	};

	const originalOpen = XMLHttpRequest.prototype.open;
	const originalSend = XMLHttpRequest.prototype.send;
	XMLHttpRequest.prototype.open = function(method, url, ...args) {
		this.__logItem = { type: 'xhr', method: method, url: url };
		window.__netLogs.push(this.__logItem);
		return originalOpen.apply(this, [method, url, ...args]);
	};
	XMLHttpRequest.prototype.send = function(body, ...args) {
		if (this.__logItem) {
			this.__logItem.reqBody = body || '';
		}
		const self = this;
		const onReadyStateChange = function() {
			if (self.readyState === 4 && self.__logItem) {
				self.__logItem.status = self.status;
				try {
					self.__logItem.resBody = (self.responseText || '').substring(0, 500);
				} catch(e) {}
			}
		};
		if (this.addEventListener) {
			this.addEventListener('readystatechange', onReadyStateChange, false);
		} else {
			const oldHandler = this.onreadystatechange;
			this.onreadystatechange = function(...handlerArgs) {
				onReadyStateChange();
				if (oldHandler) oldHandler.apply(self, handlerArgs);
			};
		}
		return originalSend.apply(this, [body, ...args]);
	};
})()`

func getLocalTempDir() string {
	tempDir := "./temp_uploads"
	_ = os.MkdirAll(tempDir, 0755)
	return tempDir
}

// sanitizeImageToTemp 将给定的图片文件（PNG/JPEG/GIF）进行解码并使用 Go 官方的纯净编码器重新写为标准的 JPEG/PNG 临时文件。
// 这可以彻底消除图片中非标准的文件元数据块、色彩空间（如 CMYK）错配，从而防止今日头条后台报错“无效图片数据”。
func sanitizeImageToTemp(srcPath string) (string, func(), error) {
	file, err := os.Open(srcPath)
	if err != nil {
		return "", nil, fmt.Errorf("failed to open source image for sanitization: %w", err)
	}
	defer file.Close()

	// 解码图像
	img, _, err := image.Decode(file)
	if err != nil {
		// 优雅降级：如果是 WebP 或其他未注册格式，打印 Warn 警告并回退直接使用原文件，不阻塞运行
		log.Warnf("【图片净化】无法解码图片 %s: %v，将跳过净化直接使用原文件", srcPath, err)
		return srcPath, func() {}, nil
	}

	// 限制最大单边尺寸为 2560 像素，防止今日头条后台因分辨率或体积过大报“无效图片数据”拒收
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	maxDim := 2560
	if width > maxDim || height > maxDim {
		var newW, newH int
		if width > height {
			newW = maxDim
			newH = int(float64(height) * float64(maxDim) / float64(width))
		} else {
			newH = maxDim
			newW = int(float64(width) * float64(maxDim) / float64(height))
		}
		log.Infof("【图片净化】图片尺寸过大 (%dx%d)，自动等比缩放为 (%dx%d) 以规避头条接收限制", width, height, newW, newH)
		newImg := image.NewRGBA(image.Rect(0, 0, newW, newH))
		for y := 0; y < newH; y++ {
			for x := 0; x < newW; x++ {
				srcX := int(float64(x) * float64(width) / float64(newW))
				srcY := int(float64(y) * float64(height) / float64(newH))
				newImg.Set(x, y, img.At(srcX+bounds.Min.X, srcY+bounds.Min.Y))
			}
		}
		img = newImg
	}

	// 无论原图是什么格式，一律统一重构为标准 JPEG 格式以消除头条后台对于 RGBA PNG 的“无效图片数据”拒收问题。
	// 为了使带有透明度通道的 PNG 转换后背景不为黑色，我们需要先创建一个白色背景层并将原图叠加绘制其上。
	bounds = img.Bounds()
	currW := bounds.Dx()
	currH := bounds.Dy()
	whiteBgImg := image.NewRGBA(image.Rect(0, 0, currW, currH))
	
	// 填充白色
	whiteColor := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	for y := 0; y < currH; y++ {
		for x := 0; x < currW; x++ {
			whiteBgImg.Set(x, y, whiteColor)
		}
	}
	
	// 将原图绘制在白色背景之上（如果带透明通道会自动融合，不带透明通道则原样覆盖）
	draw.Draw(whiteBgImg, whiteBgImg.Bounds(), img, bounds.Min, draw.Over)
	img = whiteBgImg

	// 强制以 .jpg 格式在本地临时目录创建文件
	tempFile, err := os.CreateTemp(getLocalTempDir(), "toutiaohao-clean-img-*.jpg")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp file for image sanitization: %w", err)
	}
	tempPath := tempFile.Name()
	if absPath, absErr := filepath.Abs(tempPath); absErr == nil {
		tempPath = absPath
	}

	// 采用 95 高质量重新编码 JPEG (丢弃透明通道，消除 ICC Profile 与色彩空间错配问题)
	encodeErr := jpeg.Encode(tempFile, img, &jpeg.Options{Quality: 95})
	tempFile.Close()

	if encodeErr != nil {
		_ = os.Remove(tempPath)
		log.Warnf("【图片净化】重写图片为 JPEG 失败: %v，优雅回退直接使用原文件", encodeErr)
		return srcPath, func() {}, nil
	}

	log.Infof("【图片净化成功】图片 %s 已重写为标准纯净的 jpeg 格式: %s", filepath.Base(srcPath), tempPath)
	cleanup := func() {
		_ = os.Remove(tempPath)
		log.Infof("已清理重构的临时图片文件: %s", tempPath)
	}
	return tempPath, cleanup, nil
}

func downloadImageToTemp(imgURL string) (string, func(), error) {
	// 1. 如果是本地路径，转换为绝对路径并进行净化
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

		// 净化本地图片
		cleanPath, cleanup, cleanErr := sanitizeImageToTemp(absPath)
		if cleanErr == nil {
			return cleanPath, cleanup, nil
		}
		log.Warnf("【图片净化】本地图片净化失败，回退使用原文件: %v", cleanErr)
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

	// 创建临时文件，位于本地临时目录
	tempFile, err := os.CreateTemp(getLocalTempDir(), "toutiaohao-download-img-*"+ext)
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
	if absPath, absErr := filepath.Abs(tempPath); absErr == nil {
		tempPath = absPath
	}
	log.Infof("网络图片成功下载至本地临时路径: %s", tempPath)

	// 对网络下载的图片执行净化重构
	cleanPath, cleanup, cleanErr := sanitizeImageToTemp(tempPath)
	if cleanErr == nil {
		// 净化成功后，原下载的临时文件可以直接删除
		_ = os.Remove(tempPath)
		return cleanPath, cleanup, nil
	}

	log.Warnf("【图片净化】网络图片净化失败，回退使用下载原文件: %v", cleanErr)
	cleanup = func() {
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
			// 1. 强行将 React Value 清空，通知虚拟 DOM
			_, _ = el.Eval(`() => {
				this.focus();
				let setter = null;
				let prototype = Object.getPrototypeOf(this);
				while (prototype) {
					const desc = Object.getOwnPropertyDescriptor(prototype, 'value');
					if (desc && desc.set) {
						setter = desc.set;
						break;
					}
					prototype = Object.getPrototypeOf(prototype);
				}
				if (setter) {
					setter.call(this, '');
				} else {
					this.value = '';
				}
				const tracker = this._valueTracker;
				if (tracker) {
					tracker.setValue('');
				}
				this.dispatchEvent(new Event('input', {bubbles: true}));
				this.dispatchEvent(new Event('change', {bubbles: true}));
			}`)
			time.Sleep(200 * time.Millisecond)

			// 2. 物理全选并进行原生模拟键入，确保 React 状态同步
			_ = el.SelectAllText()
			_ = el.Input(text)

			// 3. 校验输入内容是否成功且一致
			val, evalErr := el.Eval(`() => this.value || ''`)
			if evalErr == nil && val != nil && strings.TrimSpace(val.Value.Str()) == text {
				return nil
			}

			// 4. 兜底 Fallback：采用 React Value Tracker 劫持技术强行注入
			reactSetJSTemplate := `(el, val) => {
				try {
					let setter = null;
					let prototype = Object.getPrototypeOf(el);
					while (prototype) {
						const desc = Object.getOwnPropertyDescriptor(prototype, 'value');
						if (desc && desc.set) {
							setter = desc.set;
							break;
						}
						prototype = Object.getPrototypeOf(prototype);
					}
					if (setter) {
						setter.call(el, val);
					} else {
						el.value = val;
					}
					const tracker = el._valueTracker;
					if (tracker) {
						tracker.setValue(val);
					}
				} catch(e) {
					el.value = val;
				}
				el.dispatchEvent(new Event('input', {bubbles: true}));
				el.dispatchEvent(new Event('change', {bubbles: true}));
			}`

			_, err := el.Eval(`val => {
				const setter = ` + reactSetJSTemplate + `;
				setter(this, val);
			}`, text)
			return err
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
	// 刚进入时，强行在页面中注入网络请求劫持监听
	_, _ = page.Eval(NetworkTrackerJS)

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
		// 轮询读取页面内部记录的 Fetch/XHR 请求响应日志
		if netLogs, netErr := page.Eval(`() => {
			let logs = window.__netLogs || [];
			window.__netLogs = [];
			return logs;
		}`); netErr == nil && netLogs != nil {
			for _, item := range netLogs.Value.Arr() {
				log.Infof("[Browser NetLog] %s", item.String())
			}
		}

		// 捕获并输出任何可能出现的 Toast 或提示消息，帮助定位异步结果
		if toastEls, toastErr := page.Timeout(100 * time.Millisecond).Elements(`[class*="toast"], [class*="message"], [class*="notification"], [class*="alert"]`); toastErr == nil {
			for _, el := range toastEls {
				if tTxt, errTxt := el.Text(); errTxt == nil && strings.TrimSpace(tTxt) != "" {
					log.Infof("【检测到页面浮动提示】: %s", strings.TrimSpace(tTxt))
				}
			}
		}

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
		successTexts := []string{"发布成功", "已发布", "发表成功", "更新成功", "修改成功", "已更新", "更新发表成功", "定时发布成功"}
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
	_, _ = page.Eval(`() => { window.scrollTo(0, 0); }`)
	time.Sleep(1 * time.Second)
	safeScreenshot(page, "./screenshot_timeout.png")
	bodyText, _ := page.Eval(`() => document.body.innerText`)
	var textStr string
	if bodyText != nil {
		textStr = bodyText.Value.Str()
	}
	log.Warnf("发布检测超时，当前页面文本内容:\n%s", textStr)
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

	// 确保展开“发文设置”以露出定时发布选项
	_, _ = page.Timeout(3*time.Second).Eval(`() => {
		// 先看当前页面上能不能找到含有“定时发布”或“立即发布”相关的可见元素。如果能找到，说明“发文设置”可能已经是展开的，无需再次点击。
		let timeFound = Array.from(document.querySelectorAll('span, label, p, div')).find(el => {
			let text = el.textContent ? el.textContent.trim() : '';
			return (text === '定时发布' || text === '立即发布') && el.offsetWidth > 0;
		});
		
		// 如果找不到，说明没有展开，去点击“发文设置”开关
		if (!timeFound) {
			let settingsTrigger = Array.from(document.querySelectorAll('*')).find(el => {
				let text = el.textContent ? el.textContent.trim() : '';
				return (text === '发文设置' || text === '发文设置 ∨' || text === '发文设置 ^') && el.children.length <= 1;
			});
			if (settingsTrigger) {
				settingsTrigger.click();
			}
		}
	}`)
	time.Sleep(1 * time.Second)

	// 1. 定位并点击“定时发布”单选标签/大按钮
	scheduleRadioSelectors := []string{
		`//button[contains(., '定时发布')]`,
		`//button[span[contains(text(), '定时发布')]]`,
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
		let btn = this.tagName === 'BUTTON' ? this : this.closest('button') || this;
		scrollIntoViewSafe(btn);
		const events = ['pointerdown', 'mousedown', 'pointerup', 'mouseup', 'click'];
		events.forEach(name => {
			const ev = new MouseEvent(name, { bubbles: true, cancelable: true, view: window });
			btn.dispatchEvent(ev);
		});
	}`)
	time.Sleep(3000 * time.Millisecond) // 等待时间选择弹窗渲染出来

	// 2. 检查并操作定时发布弹窗（byte-select 新版结构）
	modalEl, errModal := page.Timeout(2 * time.Second).Element(`[class*='timing-picker'], .common-timing-picker, .byte-modal`)
	if errModal == nil && modalEl != nil {
		log.Info("检测到新版定时发布 Modal 弹窗存在，开始进行 Select 下拉框交互选择...")

		
		t, errParse := time.ParseInLocation("2006-01-02 15:04", timeStr, time.Local)
		if errParse != nil {
			log.Warnf("【时间】解析定时时间 %s 失败: %v，将强行隐藏弹窗进行优雅降级...", timeStr, errParse)
			_, _ = page.Eval(`() => {
				document.querySelectorAll('[class*="timing-picker"], .common-timing-picker, .byte-modal, .byte-modal-wrapper, .semi-modal-wrapper, [class*="modal-wrapper"]').forEach(el => {
					el.style.display = 'none';
					el.style.pointerEvents = 'none';
				});
			}`)
			return nil
		}

		dayTarget1 := fmt.Sprintf("%02d月%02d日", t.Month(), t.Day())
		dayTarget2 := fmt.Sprintf("%d月%d日", t.Month(), t.Day())
		hourTarget1 := fmt.Sprintf("%d", t.Hour())
		hourTarget2 := fmt.Sprintf("%02d", t.Hour())

		// (1) 交互选择日期
		daySelect, errDaySel := modalEl.Element(".day-select")
		if errDaySel != nil {
			daySelect, _ = modalEl.ElementX(`//div[contains(@class, 'byte-select') and (contains(., '月') or contains(., '日'))]`)
		}
		
		if daySelect != nil {
			log.Infof("找到日期下拉框，点击展开。")
			_ = daySelect.Click(proto.InputMouseButtonLeft, 1)
			time.Sleep(800 * time.Millisecond)

			log.Infof("匹配目标日期: %s 或 %s ...", dayTarget1, dayTarget2)
			clickedDay, errClickDay := page.Eval(`(d1, d2) => {
				let opts = Array.from(document.querySelectorAll('.byte-select-option, [class*="select-option"], li, div, span')).filter(el => {
					let text = el.textContent ? el.textContent.trim() : '';
					return (text.includes(d1) || text.includes(d2)) && el.offsetWidth > 0;
				});
				if (opts.length > 0) {
					opts[0].click();
					return true;
				}
				return false;
			}`, dayTarget1, dayTarget2)

			if errClickDay == nil && clickedDay != nil && clickedDay.Value.Bool() {
				log.Info("日期选择成功")
			} else {
				log.Warnf("未选中日期选项: %s/%s", dayTarget1, dayTarget2)
			}
			time.Sleep(500 * time.Millisecond)
		}

		// (2) 交互选择小时
		hourSelect, errHourSel := modalEl.Element(".hour-select")
		if errHourSel != nil {
			selects, _ := modalEl.Elements(`.byte-select`)
			if len(selects) >= 2 {
				hourSelect = selects[1]
			}
		}

		if hourSelect != nil {
			log.Infof("找到小时下拉框，点击展开。")
			_ = hourSelect.Click(proto.InputMouseButtonLeft, 1)
			time.Sleep(800 * time.Millisecond)

			log.Infof("匹配目标小时: %s 或 %s ...", hourTarget1, hourTarget2)
			clickedHour, errClickHour := page.Eval(`(h1, h2) => {
				let opts = Array.from(document.querySelectorAll('.byte-select-option, [class*="select-option"], li, div, span')).filter(el => {
					let text = el.textContent ? el.textContent.trim() : '';
					return (text === h1 || text === h2 || text === h1 + '时' || text === h2 + '时' || text === h1 + '点' || text === h2 + '点') && el.offsetWidth > 0;
				});
				if (opts.length > 0) {
					opts[0].click();
					return true;
				}
				let optsLoose = Array.from(document.querySelectorAll('.byte-select-option, [class*="select-option"], li, div, span')).filter(el => {
					let text = el.textContent ? el.textContent.trim() : '';
					return (text.includes(h1) || text.includes(h2)) && el.offsetWidth > 0;
				});
				if (optsLoose.length > 0) {
					optsLoose[0].click();
					return true;
				}
				return false;
			}`, hourTarget1, hourTarget2)

			if errClickHour == nil && clickedHour != nil && clickedHour.Value.Bool() {
				log.Info("小时选择成功")
			} else {
				log.Warnf("未选中小时选项: %s/%s", hourTarget1, hourTarget2)
			}
			time.Sleep(500 * time.Millisecond)
		}

		// (3) 交互选择分钟
		minuteSelect, errMinSel := modalEl.Element(".minute-select")
		if errMinSel == nil && minuteSelect != nil {
			log.Infof("找到分钟下拉框，点击展开。")
			_ = minuteSelect.Click(proto.InputMouseButtonLeft, 1)
			time.Sleep(800 * time.Millisecond)

			minuteTarget1 := fmt.Sprintf("%02d", t.Minute())
			minuteTarget2 := fmt.Sprintf("%d", t.Minute())
			log.Infof("匹配目标分钟: %s 或 %s ...", minuteTarget1, minuteTarget2)

			clickedMin, errClickMin := page.Eval(`(m1, m2) => {
				let opts = Array.from(document.querySelectorAll('.byte-select-option, [class*="select-option"], li, div, span')).filter(el => {
					let text = el.textContent ? el.textContent.trim() : '';
					return (text === m1 || text === m2 || text === m1 + '分' || text === m2 + '分') && el.offsetWidth > 0;
				});
				if (opts.length > 0) {
					opts[0].click();
					return true;
				}
				let optsLoose = Array.from(document.querySelectorAll('.byte-select-option, [class*="select-option"], li, div, span')).filter(el => {
					let text = el.textContent ? el.textContent.trim() : '';
					return (text.includes(m1) || text.includes(m2)) && el.offsetWidth > 0;
				});
				if (optsLoose.length > 0) {
					optsLoose[0].click();
					return true;
				}
				return false;
			}`, minuteTarget1, minuteTarget2)

			if errClickMin == nil && clickedMin != nil && clickedMin.Value.Bool() {
				log.Info("分钟选择成功")
			} else {
				log.Warnf("未选中分钟选项: %s/%s", minuteTarget1, minuteTarget2)
			}
			time.Sleep(500 * time.Millisecond)
		}

		// (3) 点击确定
		log.Info("确认定时选择...")
		var confirmed bool
		modalConfirmSelectors := []string{
			`button:contains('预览并定时发布')`,
			`button:contains('定时发布')`,
			`.byte-modal-footer button`,
		}
		for _, sel := range modalConfirmSelectors {
			var elConfirm *rod.Element
			var errEl error
			if strings.Contains(sel, ":contains") {
				text := strings.TrimSuffix(strings.TrimPrefix(sel, `button:contains('`), `')`)
				elConfirm, errEl = modalEl.ElementX(fmt.Sprintf(`//button[contains(., '%s')]`, text))
			} else {
				elConfirm, errEl = modalEl.Element(sel)
			}
			
			if errEl == nil && elConfirm != nil {
				_ = elConfirm.Click(proto.InputMouseButtonLeft, 1)
				confirmed = true
				break
			}
		}

		if !confirmed {
			clickedConf, errConf := page.Eval(`() => {
				let btn = Array.from(document.querySelectorAll('.byte-modal-footer button, .byte-modal button, button')).find(b => {
					let text = b.textContent ? b.textContent.trim() : '';
					return (text.includes('定时发布') || text.includes('确定') || text.includes('确认') || text.includes('提交')) && b.offsetWidth > 0;
				});
				if (btn) {
					btn.click();
					return true;
				}
				return false;
			}`)
			if errConf == nil && clickedConf != nil && clickedConf.Value.Bool() {
				confirmed = true
			}
		}

		if confirmed {
			time.Sleep(1500 * time.Millisecond)
			closed, _ := page.Eval(`() => {
				let modal = document.querySelector('[class*="timing-picker"], .common-timing-picker, .byte-modal');
				return !modal || modal.offsetWidth === 0;
			}`)
			if closed != nil && closed.Value.Bool() {
				log.Info("新版定时发布 Modal 成功设置并关闭")
				return nil
			}
		}

		// 优雅降级：直接隐藏遮挡弹窗，供底部发文大按钮展示
		log.Warn("定时发布 Modal 未正常关闭，强行隐藏以防物理遮挡...")
		_, _ = page.Eval(`() => {
			document.querySelectorAll('[class*="timing-picker"], .common-timing-picker, .byte-modal, .byte-modal-wrapper, .semi-modal-wrapper, [class*="modal-wrapper"]').forEach(el => {
				el.style.display = 'none';
				el.style.pointerEvents = 'none';
			});
		}`)
		time.Sleep(500 * time.Millisecond)
		return nil
	}

	// 3. 兜底：传统日期输入框形式
	timeInputSelectors := []string{
		`input[placeholder*='选择日期']`,
		`input[placeholder*='请选择']`,
		`input[class*='datepicker']`,
		`.byte-datepicker input`,
		`.semi-datepicker input`,
	}

	log.Info("未检测到弹窗，使用传统日期输入框兜底定位...")
	elInput, selInput, err := findElement(page, 4*time.Second, timeInputSelectors)
	if err != nil {
		safeScreenshot(page, "./screenshot_publish_time_error.png")
		return fmt.Errorf("未找到日期选择弹窗，亦未找到日期时间输入框，设置时间失败: %w", err)
	}
	log.Infof("已找到日期时间输入框，选择器: %s", selInput)

	_ = elInput.SelectAllText()
	if err := elInput.Input(timeStr); err != nil {
		return fmt.Errorf("向日期时间输入框输入时间失败: %w", err)
	}
	time.Sleep(500 * time.Millisecond)

	_, _ = elInput.Eval(`function(val) {
		this.value = val;
		this.dispatchEvent(new Event('input', { bubbles: true }));
		this.dispatchEvent(new Event('change', { bubbles: true }));
	}`, timeStr)
	time.Sleep(1000 * time.Millisecond)

	log.Info("正在确认日期时间选择...")
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



