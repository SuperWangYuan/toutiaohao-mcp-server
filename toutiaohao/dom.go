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
	"net/url"
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
	tempDir := filepath.Join(findProjectRoot(), ".tmp")
	_ = os.MkdirAll(tempDir, 0755)
	if abs, err := filepath.Abs(tempDir); err == nil {
		return abs
	}
	return tempDir
}

func findProjectRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return wd
		}
		dir = parent
	}
}

func clickVisibleModalPrimaryButton(page *rod.Page, wantedTexts []string, description string) (bool, string, error) {
	res, err := page.Eval(`(wantedTexts, description) => {
		const visible = (el) => {
			if (!el) return false;
			const style = window.getComputedStyle(el);
			const rect = el.getBoundingClientRect();
			return style.display !== 'none' &&
				style.visibility !== 'hidden' &&
				style.opacity !== '0' &&
				rect.width > 0 &&
				rect.height > 0;
		};
		const disabled = (el) => {
			const cls = String(el.className || '');
			return el.disabled ||
				el.getAttribute('disabled') !== null ||
				el.getAttribute('aria-disabled') === 'true' ||
				cls.includes('disabled') ||
				cls.includes('is-disabled') ||
				cls.includes('byte-btn-disabled') ||
				cls.includes('semi-button-disabled');
		};
		const modalSelectors = [
			'.common-timing-picker',
			'[class*="timing-picker"]',
			'.semi-modal',
			'.byte-modal',
			'[role="dialog"]',
			'[class*="modal"]',
			'[class*="dialog"]'
		];
		const modals = Array.from(document.querySelectorAll(modalSelectors.join(',')))
			.filter(el => visible(el) && (el.textContent || '').trim().length > 0);
		const debug = [];
		for (const modal of modals) {
			const modalText = (modal.textContent || '').trim();
			const actionControls = Array.from(modal.querySelectorAll('button, [role="button"], a, .byte-btn, .semi-button, [class*="btn"], [class*="button"]'));
			let controls = actionControls
				.filter(el => el !== modal && visible(el) && !disabled(el))
				.filter(el => {
					const text = (el.textContent || '').trim();
					if (!text) return false;
					if (text === modalText) return false;
					if (text === '取消' || text === '关闭' || text === '返回') return false;
					return true;
				});
			if (controls.length === 0) {
				controls = Array.from(modal.querySelectorAll('div, span'))
					.filter(el => el !== modal && visible(el) && !disabled(el))
					.filter(el => {
						const text = (el.textContent || '').trim();
						if (!text || text === modalText) return false;
						if (text === '取消' || text === '关闭' || text === '返回') return false;
						return !Array.from(el.children || []).some(child => {
							const childText = (child.textContent || '').trim();
							return childText && text.includes(childText);
						});
					});
			}
			debug.push({
				modal: (modal.className || modal.getAttribute('role') || modal.tagName || '').toString(),
				buttons: controls.map(el => (el.textContent || '').trim()).filter(Boolean).slice(0, 8)
			});

			let btn = controls.find(el => {
				const text = (el.textContent || '').trim();
				return wantedTexts.some(w => text === w);
			});
			if (!btn) {
				btn = controls.find(el => {
					const text = (el.textContent || '').trim();
					return wantedTexts.some(w => text.includes(w));
				});
			}
			if (!btn) {
				btn = controls.find(el => {
					const cls = String(el.className || '');
					return cls.includes('primary') || cls.includes('confirm');
				});
			}
			if (!btn && controls.length > 0) {
				btn = controls[controls.length - 1];
			}
			if (btn) {
				const text = (btn.textContent || '').trim();
				btn.click();
				return JSON.stringify({ clicked: true, text, description, debug });
			}
		}
		return JSON.stringify({ clicked: false, description, debug });
	}`, wantedTexts, description)
	if err != nil {
		return false, "", err
	}
	if res == nil {
		return false, "", nil
	}
	result := res.Value.Str()
	return strings.Contains(result, `"clicked":true`), result, nil
}

func clickVisiblePageButtonByText(page *rod.Page, wantedTexts []string, blockedTexts []string, description string) (bool, string, error) {
	res, err := page.Eval(`(wantedTexts, blockedTexts, description) => {
		const visible = (el) => {
			if (!el) return false;
			const style = window.getComputedStyle(el);
			const rect = el.getBoundingClientRect();
			return style.display !== 'none' &&
				style.visibility !== 'hidden' &&
				style.opacity !== '0' &&
				rect.width > 0 &&
				rect.height > 0;
		};
		const disabled = (el) => {
			const cls = String(el.className || '');
			return el.disabled ||
				el.getAttribute('disabled') !== null ||
				el.getAttribute('aria-disabled') === 'true' ||
				cls.includes('disabled') ||
				cls.includes('is-disabled') ||
				cls.includes('byte-btn-disabled') ||
				cls.includes('semi-button-disabled');
		};
		const textOf = (el) => (el.textContent || '').replace(/\s+/g, '').trim();
		const blocked = (text) => blockedTexts.some(w => text === w || text.includes(w));
		const matched = (text) => wantedTexts.some(w => {
			if (text === w) return true;
			return w.length > 1 && text.includes(w);
		});
		let controls = Array.from(document.querySelectorAll('button, [role="button"], a, .byte-btn, .semi-button, [class*="btn"], [class*="button"]'))
			.filter(el => visible(el) && !disabled(el))
			.filter(el => {
				const text = textOf(el);
				if (!text) return false;
				if (blocked(text)) return false;
				return true;
			});

		const debug = controls.map(el => ({
			tag: el.tagName,
			className: String(el.className || '').slice(0, 80),
			text: textOf(el).slice(0, 40)
		})).filter(item => item.text).slice(0, 20);

		let btn = controls.find(el => wantedTexts.some(w => textOf(el) === w));
		if (!btn) {
			btn = controls.find(el => matched(textOf(el)));
		}
		if (!btn) {
			btn = controls.find(el => {
				const cls = String(el.className || '');
				const text = textOf(el);
				return matched(text) && (cls.includes('primary') || cls.includes('confirm'));
			});
		}
		if (!btn) {
			return JSON.stringify({ clicked: false, description, debug });
		}

		const text = textOf(btn);
		btn.click();
		return JSON.stringify({ clicked: true, text, description, debug });
	}`, wantedTexts, blockedTexts, description)
	if err != nil {
		return false, "", err
	}
	if res == nil {
		return false, "", nil
	}
	result := res.Value.Str()
	return strings.Contains(result, `"clicked":true`), result, nil
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
		localPath := imgURL
		if strings.HasPrefix(imgURL, "file://") {
			u, err := url.Parse(imgURL)
			if err != nil {
				return "", func() {}, fmt.Errorf("解析 file URL 失败: %w", err)
			}
			localPath = u.Path
			if localPath == "" {
				return "", func() {}, fmt.Errorf("file URL 缺少本地路径: %s", imgURL)
			}
		}
		absPath, err := filepath.Abs(localPath)
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
				const setter = `+reactSetJSTemplate+`;
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
	"请勿选择重复的封面",
	"重复的封面",
	"为保证读者体验",
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
	maxTime := now.Add(7 * 24 * time.Hour)
	if targetTime.Before(minTime) || targetTime.After(maxTime) {
		log.Warnf("【时间提示】定时发布时间 %s 可能不符合头条平台要求。当前页面提示通常要求在“当前时间+2小时 到 7天内”之间。当前时间：%s",
			targetTime.Format("2006-01-02 15:04"), now.Format("2006-01-02 15:04"))
	}

	return targetTime.Format("2006-01-02 15:04"), nil
}

func findExistingTimingModal(page *rod.Page, stage string) (*rod.Element, error) {
	modalScan, _ := page.Eval(`() => {
		document.querySelectorAll('.mcp-timing-modal').forEach(el => el.classList.remove('mcp-timing-modal'));
		const usable = (el) => {
			if (!el) return false;
			let cur = el;
			while (cur && cur !== document.documentElement) {
				const style = window.getComputedStyle(cur);
				if (style.display === 'none' || style.visibility === 'hidden' || style.opacity === '0') return false;
				cur = cur.parentElement;
			}
			return true;
		};
		const selectors = [
			'.byte-modal-wrapper[style*="display: block"] .common-timing-picker',
			'.common-timing-picker',
			'[class*="timing-picker"]',
			'.byte-modal',
			'[role="dialog"]',
			'.semi-modal'
		];
		const seen = new Set();
		const candidates = [];
		for (const sel of selectors) {
			for (const el of document.querySelectorAll(sel)) {
				if (seen.has(el)) continue;
				seen.add(el);
				const text = (el.textContent || '').replace(/\s+/g, '').trim();
				if (!text) continue;
				candidates.push({
					el,
					selector: sel,
					text,
					usable: usable(el),
					className: String(el.className || '').slice(0, 120)
				});
			}
		}
		const modalCandidate = candidates.find(item => item.usable && (item.text.includes('定时发布') || item.text.includes('预览并定时发布')));
		if (modalCandidate) {
			modalCandidate.el.classList.add('mcp-timing-modal');
			return JSON.stringify({ found: true, selector: modalCandidate.selector, className: modalCandidate.className, text: modalCandidate.text.slice(0, 120) });
		}
		return JSON.stringify({
			found: false,
			candidates: candidates.map(item => ({
				selector: item.selector,
				className: item.className,
				usable: item.usable,
				text: item.text.slice(0, 80)
			})).slice(0, 12)
		});
	}`)
	if modalScan != nil {
		log.Infof("定时发布 Modal 扫描结果[%s]: %s", stage, modalScan.Value.Str())
	}
	return page.Timeout(2 * time.Second).Element(`.mcp-timing-modal`)
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

	modalEl, errModal := findExistingTimingModal(page, "initial")
	if errModal != nil || modalEl == nil {
		// 确保展开“发文设置”以露出定时发布选项
		_, _ = page.Timeout(3 * time.Second).Eval(`() => {
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
			`//label[contains(., '定时发布')]`,
			`//span[contains(text(), '定时发布') and not(ancestor::button)]`,
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

		// 【诊断截图与 DOM Dump】
		safeScreenshot(page, "./screenshot_after_radio_click.png")
		if htmlVal, _ := page.Eval(`() => document.body.innerHTML`); htmlVal != nil {
			_ = os.WriteFile("./dom_dump_after_radio_click.html", []byte(htmlVal.Value.Str()), 0644)
			log.Warn("已保存勾选定时发布后的瞬时 DOM Dump 至 ./dom_dump_after_radio_click.html")
		}
	}

	// 2. 检查并操作定时发布弹窗（byte-select 新版结构）
	timingModalScanJS := `() => {
		document.querySelectorAll('.mcp-timing-modal').forEach(el => el.classList.remove('mcp-timing-modal'));
		const usable = (el) => {
			if (!el) return false;
			let cur = el;
			while (cur && cur !== document.documentElement) {
				const style = window.getComputedStyle(cur);
				if (style.display === 'none' || style.visibility === 'hidden' || style.opacity === '0') return false;
				cur = cur.parentElement;
			}
			return true;
		};
		const selectors = [
			'.byte-modal-wrapper[style*="display: block"] .common-timing-picker',
			'.common-timing-picker',
			'[class*="timing-picker"]',
			'.byte-modal',
			'[role="dialog"]',
			'.semi-modal'
		];
		const seen = new Set();
		const candidates = [];
		for (const sel of selectors) {
			for (const el of document.querySelectorAll(sel)) {
				if (seen.has(el)) continue;
				seen.add(el);
				const text = (el.textContent || '').replace(/\s+/g, '').trim();
				if (!text) continue;
				candidates.push({
					el,
					selector: sel,
					text,
					usable: usable(el),
					className: String(el.className || '').slice(0, 120)
				});
			}
		}
		const modalCandidate = candidates.find(item => {
			const text = item.text;
			return item.usable && (text.includes('定时发布') || text.includes('预览并定时发布'));
		});
		if (modalCandidate) {
			modalCandidate.el.classList.add('mcp-timing-modal');
			return JSON.stringify({
				found: true,
				selector: modalCandidate.selector,
				className: modalCandidate.className,
				text: modalCandidate.text.slice(0, 120)
			});
		}
		return JSON.stringify({
			found: false,
			candidates: candidates.map(item => ({
				selector: item.selector,
				className: item.className,
				usable: item.usable,
				text: item.text.slice(0, 80)
			})).slice(0, 12)
		});
	}`
	modalScan, _ := page.Eval(timingModalScanJS)
	if modalScan != nil {
		log.Infof("定时发布 Modal 扫描结果: %s", modalScan.Value.Str())
	}
	modalEl, errModal = page.Timeout(2 * time.Second).Element(`.mcp-timing-modal`)
	if errModal != nil || modalEl == nil {
		clickedTiming, timingInfo, errTimingClick := clickVisiblePageButtonByText(page, []string{"定时发布"}, []string{"预览并定时发布", "预览并发布", "立即发布"}, "open timing modal fallback")
		if errTimingClick == nil && clickedTiming {
			log.Infof("未直接识别到定时 Modal，已点击页面定时发布触发器重试: %s", timingInfo)
			time.Sleep(1500 * time.Millisecond)
			modalScan, _ = page.Eval(timingModalScanJS)
			if modalScan != nil {
				log.Infof("定时发布 Modal 重试扫描结果: %s", modalScan.Value.Str())
			}
			modalEl, errModal = page.Timeout(2 * time.Second).Element(`.mcp-timing-modal`)
		}
	}
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
		minuteTarget1 := fmt.Sprintf("%02d", t.Minute())
		minuteTarget2 := fmt.Sprintf("%d", t.Minute())

		selectTimingValue := func(kind string, targets []string) bool {
			markRes, _ := page.Eval(`(kind) => {
				document.querySelectorAll('.mcp-timing-select-target').forEach(el => el.classList.remove('mcp-timing-select-target'));
				const visible = (el) => {
					if (!el) return false;
					const style = window.getComputedStyle(el);
					const rect = el.getBoundingClientRect();
					return style.display !== 'none' && style.visibility !== 'hidden' && rect.width >= 35 && rect.height >= 20;
				};
				const clean = (el) => (el.textContent || '').replace(/\s+/g, '').trim();
				const modal = document.querySelector('.mcp-timing-modal, .common-timing-picker, [class*="timing-picker"]');
				if (!modal) return JSON.stringify({ found: false, reason: 'no modal' });
				let controls = Array.from(modal.querySelectorAll('.byte-select, [class*="select"]'))
					.filter(visible)
					.filter(el => {
						const cls = String(el.className || '');
						if (cls.includes('option') || cls.includes('dropdown') || cls.includes('popup')) return false;
						const text = clean(el);
						return text.length > 0 && text.length <= 12;
					});
				controls = controls.filter((el, idx) => {
					const rect = el.getBoundingClientRect();
					return !controls.some((other, otherIdx) => {
						if (otherIdx === idx || !other.contains(el)) return false;
						const otherRect = other.getBoundingClientRect();
						return otherRect.width >= rect.width && otherRect.height >= rect.height;
					});
				});
				controls.sort((a, b) => {
					const ra = a.getBoundingClientRect();
					const rb = b.getBoundingClientRect();
					if (Math.abs(ra.top - rb.top) > 8) return ra.top - rb.top;
					return ra.left - rb.left;
				});
				const debug = controls.map(el => ({ text: clean(el), className: String(el.className || '').slice(0, 80) }));
				const dateControl = controls.find(el => /月.*日/.test(clean(el)));
				const numericControls = controls.filter(el => /^\d{1,2}$/.test(clean(el)));
				let target = null;
				if (kind === 'day') target = dateControl;
				if (kind === 'hour') target = numericControls[0] || controls[1];
				if (kind === 'minute') target = numericControls[1] || controls[2];
				if (!target) return JSON.stringify({ found: false, kind, debug });
				target.classList.add('mcp-timing-select-target');
				return JSON.stringify({ found: true, kind, text: clean(target), debug });
			}`, kind)
			if markRes != nil {
				log.Infof("定时 %s 下拉框定位结果: %s", kind, markRes.Value.Str())
				if !strings.Contains(markRes.Value.Str(), `"found":true`) {
					return false
				}
			}

			targetEl, errTarget := page.Timeout(1 * time.Second).Element(".mcp-timing-select-target")
			if errTarget != nil || targetEl == nil {
				return false
			}
			_, _ = targetEl.Eval(`() => {
				this.scrollIntoView({ block: 'center', inline: 'center' });
				const events = ['pointerdown', 'mousedown', 'pointerup', 'mouseup', 'click'];
				events.forEach(name => this.dispatchEvent(new MouseEvent(name, { bubbles: true, cancelable: true, view: window })));
			}`)
			time.Sleep(800 * time.Millisecond)

			clickRes, _ := page.Eval(`(targets) => {
				const visible = (el) => {
					if (!el) return false;
					const style = window.getComputedStyle(el);
					const rect = el.getBoundingClientRect();
					return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
				};
				const clean = (el) => (el.textContent || '').replace(/\s+/g, '').trim();
				const wanted = new Set(targets.map(t => String(t).replace(/\s+/g, '').trim()).filter(Boolean));
				let options = Array.from(document.querySelectorAll('.byte-select-option, [class*="select-option"], [role="option"], li, div, span'))
					.filter(visible)
					.filter(el => {
						const text = clean(el);
						if (!text || text.length > 12) return false;
						return wanted.has(text);
					});
				options = options.filter(el => !Array.from(el.children || []).some(child => wanted.has(clean(child))));
				const debug = options.map(el => ({ text: clean(el), className: String(el.className || '').slice(0, 80) })).slice(0, 10);
				const opt = options[0];
				if (!opt) return JSON.stringify({ clicked: false, targets, debug });
				opt.scrollIntoView({ block: 'center', inline: 'center' });
				const events = ['pointerdown', 'mousedown', 'pointerup', 'mouseup', 'click'];
				events.forEach(name => opt.dispatchEvent(new MouseEvent(name, { bubbles: true, cancelable: true, view: window })));
				return JSON.stringify({ clicked: true, text: clean(opt), targets, debug });
			}`, targets)
			if clickRes != nil {
				log.Infof("定时 %s 选项点击结果: %s", kind, clickRes.Value.Str())
				if strings.Contains(clickRes.Value.Str(), `"clicked":true`) {
					time.Sleep(600 * time.Millisecond)
					return true
				}
			}
			return false
		}

		robustSelectOK := selectTimingValue("day", []string{dayTarget1, dayTarget2}) &&
			selectTimingValue("hour", []string{hourTarget1, hourTarget2, hourTarget1 + "时", hourTarget2 + "时"}) &&
			selectTimingValue("minute", []string{minuteTarget1, minuteTarget2, minuteTarget1 + "分", minuteTarget2 + "分"})
		if robustSelectOK {
			log.Info("定时发布时间新下拉选择流程完成，跳过旧选择兜底")
		}

		if !robustSelectOK {
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
			if errMinSel != nil {
				selects, _ := modalEl.Elements(`.byte-select`)
				if len(selects) >= 3 {
					minuteSelect = selects[2]
					errMinSel = nil
				}
			}
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
		}

		// (3) 点击确定
		expectedTime := t.Format("2006-01-02 15:04")
		verifyRes, _ := page.Eval(`(expected) => {
			const normalize = (s) => String(s || '').replace(/\s+/g, '').trim();
			const expectedCompact = normalize(expected);
			const modal = document.querySelector('.mcp-timing-modal, .common-timing-picker, [class*="timing-picker"]');
			const text = normalize(modal ? modal.textContent : document.body.innerText);
			const publishTime = normalize((modal && modal.querySelector('.publish-time')) ? modal.querySelector('.publish-time').textContent : '');
			return JSON.stringify({
				matched: text.includes(expectedCompact) || publishTime.includes(expectedCompact),
				expected: expectedCompact,
				publishTime,
				text: text.slice(0, 160)
			});
		}`, expectedTime)
		if verifyRes != nil {
			verifyStr := verifyRes.Value.Str()
			log.Infof("定时发布时间设置校验: %s", verifyStr)
			if !strings.Contains(verifyStr, `"matched":true`) {
				safeScreenshot(page, "./screenshot_publish_time_verify_error.png")
				return fmt.Errorf("定时发布时间未设置为目标值 %s，页面状态: %s", expectedTime, verifyStr)
			}
		}

		log.Info("确认定时选择...")
		confirmed, confirmInfo, errConfirm := clickVisibleModalPrimaryButton(page, []string{"预览并定时发布", "定时发布", "确定", "确认", "提交"}, "timing publish modal")
		if errConfirm != nil {
			log.Warnf("点击定时发布 Modal 主按钮失败: %v", errConfirm)
		} else {
			log.Infof("定时发布 Modal 主按钮点击结果: %s", confirmInfo)
		}

		if confirmed {
			time.Sleep(1500 * time.Millisecond)
			closed, _ := page.Eval(`() => {
				const visible = (el) => {
					if (!el) return false;
					const style = window.getComputedStyle(el);
					const rect = el.getBoundingClientRect();
					return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
				};
				let modal = document.querySelector('.mcp-timing-modal, [class*="timing-picker"], .common-timing-picker');
				return !visible(modal);
			}`)
			if closed != nil && closed.Value.Bool() {
				log.Info("新版定时发布 Modal 成功设置并关闭")
				return nil
			}
		}

		log.Warn("定时发布 Modal 未正常关闭，将按项目约定降级为普通发布，但不会强行隐藏真实弹窗")
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

// ensureSettingsExpanded 确保页面底部的“发文设置”抽屉处于展开状态。
// 它会根据内部特征元素的可见性智能决策是否需要点击开关，防止已展开的状态被反向折叠收起。
func ensureSettingsExpanded(page *rod.Page) {
	log.Info("正在检查“发文设置”抽屉展开状态...")
	_, _ = page.Timeout(3 * time.Second).Eval(`() => {
		// 1. 检测“单图”、“三图”或“投放广告”等专属折叠元素是否已可见
		let expandedMarker = Array.from(document.querySelectorAll('span, label, div')).find(el => {
			let text = el.textContent ? el.textContent.trim() : '';
			return (text === '单图' || text === '三图' || text === '投放广告') && el.offsetWidth > 0;
		});
		if (expandedMarker) {
			return true; // 已展开，无需点击
		}

		// 2. 若不可见，寻找“发文设置”触发标签并执行点击展开
		let settingsTrigger = Array.from(document.querySelectorAll('*')).find(el => {
			let text = el.textContent ? el.textContent.trim() : '';
			return (text === '发文设置' || text.includes('发文设置')) && 
			       (text.includes('∨') || text.includes('down') || !text.includes('^')) &&
			       el.children.length <= 1 && 
			       el.offsetWidth > 0;
		});
		if (settingsTrigger) {
			settingsTrigger.click();
			return false; // 触发了点击
		}
		return false;
	}`)
	time.Sleep(1200 * time.Millisecond) // 等待展开动画播放完毕
}
