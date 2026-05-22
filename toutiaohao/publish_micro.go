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
func (a *MicroPostAction) Publish(ctx context.Context, content string, images []string, topic string) error {
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

	// 检查是否被重定向到登录页
	info, _ := a.page.Info()
	if info != nil && (strings.Contains(info.URL, "login") || strings.Contains(info.URL, "auth")) {
		return fmt.Errorf("not logged in, redirected to: %s", info.URL)
	}

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

	// 先尝试直接找到 file input
	fileInput, err := a.page.Timeout(2 * time.Second).Element(`input[type='file']`)
	if err == nil && fileInput != nil {
		for _, img := range images {
			_ = fileInput.SetFiles([]string{img})
			time.Sleep(3 * time.Second)
		}
		return nil
	}

	// 找不到 file input，尝试点击上传按钮
	btnEl, sel, err := findElement(a.page, 3*time.Second, uploadButtonSelectors)
	if err != nil {
		return fmt.Errorf("no upload element found: %w", err)
	}
	log.Infof("Found upload button using selector: %s", sel)
	_ = btnEl.Click(proto.InputMouseButtonLeft, 1)
	time.Sleep(1 * time.Second)

	// 点击按钮后再找 file input
	fileInput, err = a.page.Timeout(3 * time.Second).Element(`input[type='file']`)
	if err != nil || fileInput == nil {
		return fmt.Errorf("file input not found after clicking upload button")
	}

	for _, img := range images {
		_ = fileInput.SetFiles([]string{img})
		time.Sleep(3 * time.Second)
	}
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
