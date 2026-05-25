package toutiaohao

import (
	"context"
	"encoding/json"
	"fmt"
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
			log.Warnf("Image upload failed: %v", err)
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
	// 图片上传按钮选择器
	uploadButtonSelectors := []string{
		`//button[contains(@title, '图片')]`,
		`//span[contains(text(), '图片')]/ancestor::button`,
	}

	// 1. 处理图片路径（将本地相对路径规范为绝对路径，或自动下载 HTTP/HTTPS 网络图片）
	var localImages []string
	var cleanups []func()
	defer func() {
		// 在全部上传动作完成后统一释放/清理临时下载的图片文件
		for _, cleanup := range cleanups {
			cleanup()
		}
	}()

	for _, img := range images {
		localPath, cleanup, err := downloadImageToTemp(img)
		if err != nil {
			return fmt.Errorf("准备图片失败 (%s): %w", img, err)
		}
		localImages = append(localImages, localPath)
		cleanups = append(cleanups, cleanup)
	}

	// 2. 物理点击“图片”按钮以激活微头条图片上传弹窗
	btnEl, sel, err := findElement(a.page, 3*time.Second, uploadButtonSelectors)
	if err != nil {
		return fmt.Errorf("未找到图片上传按钮: %w", err)
	}
	log.Infof("Found upload button using selector: %s", sel)
	// 使用 JS 点击以防止 physical Click 导致的协程挂起阻塞
	_, _ = btnEl.Eval(`() => this.click()`)
	time.Sleep(2 * time.Second)

	// 3. 寻找上传弹窗中激活的 file input（优先在按钮祖先节点范围内寻找，防范误抓全局其它隐藏的 file input）
	var fileInput *rod.Element
	curr := btnEl
	for k := 0; k < 5; k++ {
		parent, err := curr.Parent()
		if err != nil || parent == nil {
			break
		}
		curr = parent
		el, err := curr.Element(`input[type='file']`)
		if err == nil && el != nil {
			fileInput = el
			log.Infof("在上传按钮向上第 %d 层的祖先节点中匹配到了微头条专用的 file input", k+1)
			break
		}
	}

	// 兜底：若局部未找到，再全局查找
	if fileInput == nil {
		log.Warn("在上传按钮局部区域内未找到 file input，尝试全局寻找...")
		el, err := a.page.Timeout(3 * time.Second).Element(`input[type='file']`)
		if err == nil && el != nil {
			fileInput = el
		}
	}

	if fileInput == nil {
		return fmt.Errorf("点击上传按钮后未找到 file input 控件")
	}
	fileInput = fileInput.CancelTimeout()

	// 4. 一次性上传所有图片（防止多次 SetFiles 产生文件覆盖）
	log.Infof("正在向 file input 设置全部 %d 张图片...", len(localImages))
	if err := fileInput.SetFiles(localImages); err != nil {
		return fmt.Errorf("fileInput.SetFiles 失败: %w", err)
	}

	// 5. 等待图片上传在弹窗中渲染完成
	log.Info("等待图片上传并生成缩略图...")
	time.Sleep(5 * time.Second)

	// 6. 点击弹窗右下角的“确定”按钮以完成插入，循环检测直到弹窗成功关闭
	log.Info("正在循环寻找并点击图片弹窗的“确定”按钮...")
	confirmed := false
	for k := 0; k < 10; k++ {
		res, errEval := a.page.Eval(`() => {
			// 精确寻找可见的“确定”或“确认”按钮
			let elements = Array.from(document.querySelectorAll('button, span, div, a'));
			let btn = elements.find(el => {
				let text = el.textContent ? el.textContent.trim() : '';
				let isBtnLike = el.tagName === 'BUTTON' || 
				                el.classList.contains('byte-btn') || 
				                el.classList.contains('semi-button') || 
				                el.getAttribute('role') === 'button';
				// 按钮必须可见，且文本为确切的“确定”或“确认”
				return (text === '确定' || text === '确认') && isBtnLike && el.offsetWidth > 0;
			});
			if (btn) {
				btn.click();
				return true; // 找到并触发点击
			}
			return false; // 未找到，可能弹窗已关闭
		}`)

		if errEval != nil {
			log.Warnf("在图片弹窗中检测/点击确定按钮时发生 JS 错误: %v", errEval)
		} else if res != nil {
			if !res.Value.Bool() {
				// 返回 false 代表页面已无可见的“确定”按钮，弹窗已成功关闭
				log.Info("页面已无可见的图片确定按钮，确定弹窗成功关闭")
				confirmed = true
				break
			} else {
				log.Infof("已定位到确定按钮并触发点击 (尝试次数: %d/10)...", k+1)
			}
		}
		time.Sleep(1 * time.Second)
	}

	if !confirmed {
		return fmt.Errorf("未能成功确认并关闭图片上传弹窗（确定按钮在 10 秒内未消失，可能是上传卡死或按钮无效）")
	}

	log.Info("图片已成功确认并插入微头条编辑框")
	time.Sleep(2 * time.Second)
	return nil
}

// clickPublish 点击发布按钮
func (a *MicroPostAction) clickPublish() error {
	if err := clickFirst(a.page, 3*time.Second, MicroPublishButtonSelectors, "publish button"); err != nil {
		return err
	}

	// 等待并检测发布结果
	if err := waitForPublishResult(a.page, 8*time.Second); err != nil {
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
