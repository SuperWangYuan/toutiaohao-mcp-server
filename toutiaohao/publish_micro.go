package toutiaohao

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/example/toutiaohao-mcp-server/configs"
	"github.com/example/toutiaohao-mcp-server/cookies"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	log "github.com/sirupsen/logrus"
)

// ValidateMicroPost 校验微头条参数
func ValidateMicroPost(content string, images []string, topic string) error {
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("content is required")
	}
	if utf8.RuneCountInString(content) > configs.MaxWeitoutiaoLength {
		return fmt.Errorf("content exceeds %d characters limit", configs.MaxWeitoutiaoLength)
	}
	if len(images) > configs.MaxImagesPerPost {
		return fmt.Errorf("images count exceeds %d limit", configs.MaxImagesPerPost)
	}
	return nil
}

// MicroPostAction 微头条发布操作
type MicroPostAction struct {
	page        *rod.Page
	cookieStore cookies.Cookier
}

// NewMicroPostAction 创建微头条发布操作
func NewMicroPostAction(page *rod.Page, cookieStore cookies.Cookier) *MicroPostAction {
	return &MicroPostAction{page: page, cookieStore: cookieStore}
}

// Publish 发布微头条
func (a *MicroPostAction) Publish(ctx context.Context, content string, images []string, topic string, publishTime interface{}) error {
	if err := ValidateMicroPost(content, images, topic); err != nil {
		return err
	}

	// 拼接话题
	fullContent := content
	if topic != "" {
		if !strings.HasPrefix(topic, "#") {
			topic = "#" + topic + "#"
		}
		fullContent = topic + " " + content
	}

	// 导航到微头条发布页
	log.Info("Navigating to micro post publish page...")
	if err := a.page.Navigate(configs.PublishMicro); err != nil {
		return fmt.Errorf("failed to navigate: %w", err)
	}
	if err := a.page.WaitLoad(); err != nil {
		return fmt.Errorf("failed to wait load: %w", err)
	}
	time.Sleep(2 * time.Second)

	// 检查并处理登录状态 (包含扫码等待)
	if err := EnsureLogin(a.page, a.cookieStore); err != nil {
		return err
	}

	// 重新导航到微头条发布页以确保页面处于已登录下的正确渲染
	if err := a.page.Navigate(configs.PublishMicro); err != nil {
		return fmt.Errorf("failed to navigate: %w", err)
	}
	_ = a.page.Timeout(15 * time.Second).WaitLoad()
	time.Sleep(2 * time.Second)

	// 查找编辑器并输入内容
	if err := a.inputContent(fullContent); err != nil {
		return fmt.Errorf("failed to input content: %w", err)
	}

	// 上传图片
	if len(images) > 0 {
		if err := a.uploadImages(images); err != nil {
			return fmt.Errorf("图片上传失败: %w", err)
		}
	}

	// 今日头条微头条网页端不支持定时发布，此处仅打印警告并忽略
	if publishTime != nil {
		hasTime := false
		switch t := publishTime.(type) {
		case string:
			if t != "" {
				hasTime = true
			}
		case int, int64, float64:
			hasTime = true
		}
		if hasTime {
			log.Warn("今日头条微头条网页端不支持定时发布功能，定时发布参数已被忽略")
		}
	}

	// 点击发布按钮
	if err := a.clickPublish(); err != nil {
		return fmt.Errorf("failed to publish: %w", err)
	}

	log.Info("Micro post published successfully")
	return nil
}

// inputContent 在编辑器中输入内容
func (a *MicroPostAction) inputContent(content string) error {
	el, sel, err := findElement(a.page, 3*time.Second, MicroEditorSelectors)
	if err != nil {
		return fmt.Errorf("no editor element found: %w", err)
	}
	log.Infof("Found editor using selector: %s", sel)
	if err := inputTextWithFallback(el, content); err != nil {
		return fmt.Errorf("failed to set editor content: %w", err)
	}
	log.Info("Content input successful")
	return nil
}

// uploadImages 上传图片
func (a *MicroPostAction) uploadImages(images []string) error {
	for idx, img := range images {
		logMicroMemoryStats(fmt.Sprintf("准备上传微头条图片 %d/%d 前", idx+1, len(images)))
		localPath, cleanup, err := downloadImageToTemp(img)
		if err != nil {
			return fmt.Errorf("准备图片失败 (%s): %w", img, err)
		}
		if err := a.uploadSingleMicroImage(localPath, idx+1, len(images)); err != nil {
			cleanup()
			return err
		}
		cleanup()
		logMicroMemoryStats(fmt.Sprintf("完成上传微头条图片 %d/%d 后", idx+1, len(images)))
	}

	log.Info("微头条图片已全部成功插入")
	return nil
}

func (a *MicroPostAction) uploadSingleMicroImage(localPath string, index, total int) error {
	const chooserTimeout = 15 * time.Second
	setFiles, err := a.page.HandleFileDialog()
	if err != nil {
		return fmt.Errorf("初始化微头条 Chrome 文件选择器拦截失败: %w", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- setFiles([]string{localPath})
	}()

	if err := a.clickMicroImageUploadButton(5 * time.Second); err != nil {
		_ = proto.PageSetInterceptFileChooserDialog{Enabled: false}.Call(a.page)
		return fmt.Errorf("点击微头条图片按钮失败: %w", err)
	}

	if completed, err := waitFileChooserDone(done, 2*time.Second); completed {
		if err != nil {
			return fmt.Errorf("Chrome 文件选择器写入微头条图片失败: %w", err)
		}
		log.Infof("Chrome 文件选择器已接收微头条图片 %d/%d: %s", index, total, localPath)
	} else {
		if err := a.clickMicroLocalUploadTrigger("mcp-micro-local-upload-trigger", 8*time.Second); err != nil {
			_ = proto.PageSetInterceptFileChooserDialog{Enabled: false}.Call(a.page)
			return fmt.Errorf("未找到微头条上传面板的本地上传按钮: %w", err)
		}
		select {
		case err := <-done:
			if err != nil {
				return fmt.Errorf("Chrome 文件选择器写入微头条图片失败: %w", err)
			}
			log.Infof("Chrome 文件选择器已接收微头条图片 %d/%d: %s", index, total, localPath)
		case <-time.After(chooserTimeout):
			_ = proto.PageSetInterceptFileChooserDialog{Enabled: false}.Call(a.page)
			return fmt.Errorf("等待微头条 Chrome 文件选择器弹出超时（%s）", chooserTimeout)
		}
	}

	if err := a.waitAndConfirmMicroImageUpload(index); err != nil {
		return err
	}
	return nil
}

func (a *MicroPostAction) clickMicroImageUploadButton(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastScan string
	for time.Now().Before(deadline) {
		res, _ := a.page.Eval(`() => {
			document.querySelectorAll('.mcp-micro-upload-button').forEach(el => el.classList.remove('mcp-micro-upload-button'));
			const visible = (el) => {
				if (!el) return false;
				const style = window.getComputedStyle(el);
				const rect = el.getBoundingClientRect();
				return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
			};
			const clean = (el) => (el.textContent || el.getAttribute('title') || el.getAttribute('aria-label') || '').replace(/\s+/g, '').trim();
			const candidates = Array.from(document.querySelectorAll('button, [role="button"], label, [title*="图片"], [aria-label*="图片"]')).filter(visible);
			const debug = candidates.map(el => ({tag: el.tagName, className: String(el.className || '').slice(0, 80), text: clean(el).slice(0, 30)})).slice(0, 12);
			let target = candidates.find(el => clean(el) === '图片' || clean(el) === '插入图片' || clean(el).includes('图片'));
			if (!target) {
				const span = Array.from(document.querySelectorAll('span')).filter(visible).find(el => clean(el).includes('图片'));
				if (span) target = span.closest('button, [role="button"], label') || span;
			}
			if (target) {
				target.scrollIntoView({ block: 'center', inline: 'center' });
				target.classList.add('mcp-micro-upload-button');
				return JSON.stringify({found:true, target:{tag:target.tagName, className:String(target.className || '').slice(0, 80), text:clean(target).slice(0, 40)}, debug});
			}
			return JSON.stringify({found:false, debug});
		}`)
		if res != nil {
			lastScan = res.Value.Str()
		}
		if err := physicalClickMarkedElement(a.page, ".mcp-micro-upload-button"); err == nil {
			log.Infof("微头条图片按钮点击成功，扫描结果: %s", lastScan)
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("未找到微头条图片上传按钮，最后扫描结果: %s", lastScan)
}

func (a *MicroPostAction) clickMicroLocalUploadTrigger(markerClass string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastScan string
	for time.Now().Before(deadline) {
		res, _ := a.page.Eval(`(markerClass) => {
			document.querySelectorAll('.' + markerClass).forEach(el => el.classList.remove(markerClass));
			const visible = (el) => {
				if (!el) return false;
				const style = window.getComputedStyle(el);
				const rect = el.getBoundingClientRect();
				return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
			};
			const clean = (el) => (el.textContent || el.getAttribute('title') || el.getAttribute('aria-label') || '').replace(/\s+/g, '').trim();
			const roots = Array.from(document.querySelectorAll(
				'.upload-image-panel, .upload-handler-drag, .pgc-ic-image-tab-scope, .mp-ic-img-drawer, ' +
				'.byte-drawer, .semi-drawer, .byte-modal, .semi-modal, [role="dialog"], ' +
				'[class*="modal"], [class*="dialog"], [class*="drawer"]'
			)).filter(visible);
			const debug = [];
			for (const root of roots) {
				const controls = Array.from(root.querySelectorAll('button, [role="button"], label, .upload-handler, input[type="file"]'))
					.filter(el => visible(el) || el.matches('input[type="file"]'));
				debug.push({
					root: String(root.className || root.getAttribute('role') || root.tagName).slice(0, 80),
					text: clean(root).slice(0, 60),
					controls: controls.map(el => ({tag: el.tagName, className: String(el.className || '').slice(0, 60), text: clean(el).slice(0, 30)})).slice(0, 8)
				});
				let trigger = controls.find(el => el.matches('button, [role="button"], label') && clean(el) === '本地上传');
				if (!trigger) {
					trigger = controls.find(el => el.matches('button, [role="button"], label') && clean(el).startsWith('本地上传') && !clean(el).includes('扫码上传'));
				}
				if (!trigger) {
					const input = Array.from(root.querySelectorAll('input[type="file"]')).find(input => {
						const owner = input.closest('button, label, [role="button"]');
						return owner && clean(owner).startsWith('本地上传') && !clean(owner).includes('扫码上传');
					});
					if (input) trigger = input.closest('button, label, [role="button"]');
				}
				if (trigger) {
					trigger.scrollIntoView({ block: 'center', inline: 'center' });
					trigger.classList.add(markerClass);
					return JSON.stringify({found:true, trigger:{tag:trigger.tagName, className:String(trigger.className || '').slice(0, 80), text:clean(trigger).slice(0, 40)}, debug});
				}
			}
			return JSON.stringify({found:false, debug});
		}`, markerClass)
		if res != nil {
			lastScan = res.Value.Str()
		}
		if err := physicalClickMarkedElement(a.page, "."+markerClass); err == nil {
			log.Infof("微头条本地上传按钮点击成功，扫描结果: %s", lastScan)
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("未找到微头条本地上传触发按钮，最后扫描结果: %s", lastScan)
}

func (a *MicroPostAction) waitAndConfirmMicroImageUpload(index int) error {
	log.Infof("等待微头条图片 %d 上传并生成缩略图...", index)
	deadline := time.Now().Add(45 * time.Second)
	confirmed := false
	for time.Now().Before(deadline) {
		result, errEval := a.page.Eval(`() => {
			document.querySelectorAll('.mcp-micro-image-confirm').forEach(el => el.classList.remove('mcp-micro-image-confirm'));
			const visible = (el) => {
				if (!el) return false;
				const style = window.getComputedStyle(el);
				const rect = el.getBoundingClientRect();
				return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
			};
			const clean = (el) => (el.textContent || el.getAttribute('title') || el.getAttribute('aria-label') || '').replace(/\s+/g, '').trim();
			const roots = Array.from(document.querySelectorAll(
				'.upload-image-panel, .upload-handler-drag, .pgc-ic-image-tab-scope, .mp-ic-img-drawer, ' +
				'.byte-drawer, .semi-drawer, .byte-modal, .semi-modal, [role="dialog"], ' +
				'[class*="modal"], [class*="dialog"], [class*="drawer"]'
			)).filter(visible);
			if (!roots.length) {
				return JSON.stringify({status:'closed'});
			}
			for (const root of roots) {
				const busyText = clean(root);
				if (busyText.includes('上传中') || busyText.includes('处理中') || busyText.includes('正在上传')) {
					return JSON.stringify({status:'uploading', text:busyText.slice(0, 80)});
				}
				const buttons = Array.from(root.querySelectorAll('button, [role="button"], a')).filter(visible);
				const btn = buttons.find(el => {
					const text = clean(el);
					return (text === '确定' || text === '确认' || text === '完成') && !el.disabled && el.getAttribute('aria-disabled') !== 'true';
				});
				if (btn) {
					btn.scrollIntoView({ block: 'center', inline: 'center' });
					btn.classList.add('mcp-micro-image-confirm');
					return JSON.stringify({status:'marked', text:clean(btn)});
				}
			}
			return JSON.stringify({status:'waiting', roots: roots.map(root => clean(root).slice(0, 60)).slice(0, 5)});
		}`)
		if errEval != nil {
			log.Warnf("在微头条图片弹窗中检测确定按钮时发生 JS 错误: %v", errEval)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		status := ""
		if result != nil {
			var info struct {
				Status string `json:"status"`
				Text   string `json:"text"`
			}
			_ = json.Unmarshal([]byte(result.Value.Str()), &info)
			status = info.Status
			if info.Text != "" {
				log.Infof("微头条图片上传弹窗状态: %s %s", info.Status, info.Text)
			}
		}
		switch status {
		case "closed":
			log.Infof("微头条图片 %d 上传弹窗已关闭", index)
			return nil
		case "marked":
			if err := physicalClickMarkedElement(a.page, ".mcp-micro-image-confirm"); err != nil {
				return fmt.Errorf("点击微头条图片确定按钮失败: %w", err)
			}
			confirmed = true
			time.Sleep(800 * time.Millisecond)
		default:
			time.Sleep(800 * time.Millisecond)
		}
	}

	if !confirmed {
		return fmt.Errorf("微头条图片上传弹窗在 45 秒内未出现可点击确定按钮")
	}
	return fmt.Errorf("微头条图片确定按钮已点击，但弹窗在 45 秒内未关闭")
}

func logMicroMemoryStats(stage string) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	log.Infof("[微头条图片内存] %s: alloc=%dMB sys=%dMB heap=%dMB", stage, mem.Alloc/1024/1024, mem.Sys/1024/1024, mem.HeapAlloc/1024/1024)
}

// clickPublish 点击发布按钮
func (a *MicroPostAction) clickPublish() error {
	if err := clickFirst(a.page, 3*time.Second, MicroPublishButtonSelectors, "publish button"); err != nil {
		return err
	}

	// 等待并检测发布结果
	if err := waitForPublishResult(a.page, 30*time.Second); err != nil {
		return err
	}

	// 检查确认弹窗
	confirmSelectors := []string{
		`//button[contains(text(), '确定')]`,
		`//button[contains(text(), '确认')]`,
	}
	if ce, _, err := findElement(a.page, 1*time.Second, confirmSelectors); err == nil && ce != nil {
		_ = ce.Click(proto.InputMouseButtonLeft, 1)
		time.Sleep(2 * time.Second)
	}

	return nil
}

// DraftSaveRequest 草稿保存请求
type DraftSaveRequest struct {
	DraftType int    `json:"draft_type"`
	DraftInfo string `json:"draft_info"`
	PostID    string `json:"post_id"`
}

// DraftInfo 草稿详情
type DraftInfo struct {
	Schema          string       `json:"schema"`
	Extra           DraftExtra   `json:"extra"`
	Content         string       `json:"content"`
	ContentRichSpan string       `json:"content_rich_span"`
	Images          []DraftImage `json:"images"`
	UpdateTime      int64        `json:"update_time"`
}

// DraftExtra 草稿扩展信息
type DraftExtra struct {
	ClaimExclusive         string `json:"claim_exclusive"`
	TuwenWttTransferSwitch string `json:"tuwen_wtt_transfer_switch"`
}

// DraftImage 草稿图片信息
type DraftImage struct {
	URL      string `json:"url"`
	WebURI   string `json:"web_uri"`
	URI      string `json:"uri"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	MimeType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
}

// BuildDraftRequest 构建草稿保存请求
func BuildDraftRequest(content string, images []DraftImage) *DraftSaveRequest {
	info := DraftInfo{
		Schema: "sslocal://send_thread",
		Extra: DraftExtra{
			ClaimExclusive:         "1",
			TuwenWttTransferSwitch: "1",
		},
		Content:         content,
		ContentRichSpan: `{"links":[]}`,
		Images:          images,
		UpdateTime:      time.Now().Unix(),
	}
	if info.Images == nil {
		info.Images = []DraftImage{}
	}

	infoJSON, _ := json.Marshal(info)
	return &DraftSaveRequest{
		DraftType: 1,
		DraftInfo: string(infoJSON),
		PostID:    "",
	}
}
