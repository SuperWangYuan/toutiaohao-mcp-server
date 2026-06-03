package toutiaohao

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/example/toutiaohao-mcp-server/configs"
	"github.com/example/toutiaohao-mcp-server/cookies"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	log "github.com/sirupsen/logrus"
)

var starNullRe = regexp.MustCompile(`"([^"]*star[^"]*)"\s*:\s*null`)

func shouldBypassHijack(reqURL, method, contentType string) bool {
	lowerURL := strings.ToLower(reqURL)
	lowerContentType := strings.ToLower(contentType)
	if !strings.Contains(lowerURL, "toutiao.com") {
		return true
	}
	if strings.Contains(lowerURL, ".js") ||
		strings.Contains(lowerURL, ".css") ||
		strings.Contains(lowerURL, ".png") ||
		strings.Contains(lowerURL, ".jpg") ||
		strings.Contains(lowerURL, ".woff") {
		return true
	}

	// 图片上传请求必须让 Chrome 原样发送。通过 ctx.LoadResponse 代理 multipart body
	// 会破坏文件内容，导致头条后台只收到约 210 字节并提示“无效图片数据”。
	if strings.Contains(lowerURL, "/spice/image") ||
		strings.Contains(lowerURL, "upload_source=") ||
		strings.Contains(lowerContentType, "multipart/form-data") ||
		strings.Contains(lowerContentType, "application/octet-stream") {
		return true
	}

	return false
}

// ArticleOptions 文章发布可选参数
type ArticleOptions struct {
	Images      []string    `json:"images,omitempty"`
	Tags        []string    `json:"tags,omitempty"`
	Category    string      `json:"category,omitempty"`
	CoverImage  string      `json:"cover_image,omitempty"`
	Original    bool        `json:"original,omitempty"`
	Fiction     bool        `json:"fiction,omitempty"`
	PublishTime interface{} `json:"publish_time,omitempty"`
	SaveAsDraft bool        `json:"save_as_draft,omitempty"`
}

// ValidateArticle 校验文章参数
func ValidateArticle(title, content string, opts *ArticleOptions) error {
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("title is required")
	}
	if utf8.RuneCountInString(title) > configs.MaxTitleLength {
		return fmt.Errorf("title exceeds %d characters limit", configs.MaxTitleLength)
	}
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("content is required")
	}
	if utf8.RuneCountInString(content) > configs.MaxContentLength {
		return fmt.Errorf("content exceeds %d characters limit", configs.MaxContentLength)
	}
	return nil
}

// ValidateUpdateArticle 校验修改文章参数
func ValidateUpdateArticle(articleID, title, content string, opts *ArticleOptions) error {
	if strings.TrimSpace(articleID) == "" {
		return fmt.Errorf("article_id is required")
	}
	if title != "" && utf8.RuneCountInString(title) > configs.MaxTitleLength {
		return fmt.Errorf("title exceeds %d characters limit", configs.MaxTitleLength)
	}
	if content != "" && utf8.RuneCountInString(content) > configs.MaxContentLength {
		return fmt.Errorf("content exceeds %d characters limit", configs.MaxContentLength)
	}
	return nil
}

// ArticlePublishAction 文章发布操作
type ArticlePublishAction struct {
	page        *rod.Page
	cookieStore cookies.Cookier
}

// NewArticlePublishAction 创建文章发布操作
func NewArticlePublishAction(page *rod.Page, cookieStore cookies.Cookier) *ArticlePublishAction {
	return &ArticlePublishAction{page: page, cookieStore: cookieStore}
}

// Publish 发布文章
func (a *ArticlePublishAction) Publish(ctx context.Context, title, content string, opts *ArticleOptions) error {
	if err := ValidateArticle(title, content, opts); err != nil {
		return err
	}

	// 启用网络请求劫持，拦截并过滤星图 null 响应，防御前端 JS 崩溃
	router := a.page.HijackRequests()
	_ = router.Add("*", "*", func(ctx *rod.Hijack) {
		reqURL := ctx.Request.URL().String()

		// 【新增诊断】如果是上传图片的 API 请求，打印它的 Payload 大小和 Headers
		if strings.Contains(reqURL, "upload") || strings.Contains(reqURL, "media") {
			log.Infof("[HTTP Upload Hijack] URL: %s, Method: %s, Content-Length: %s, BodyLen: %d",
				reqURL, ctx.Request.Method(), ctx.Request.Header("Content-Length"), len(ctx.Request.Body()))
		}

		if shouldBypassHijack(reqURL, ctx.Request.Method(), ctx.Request.Header("Content-Type")) {
			ctx.ContinueRequest(&proto.FetchContinueRequest{})
			return
		}
		err := ctx.LoadResponse(http.DefaultClient, true)
		if err != nil {
			return
		}
		body := ctx.Response.Body()

		if strings.Contains(body, "star") {
			body = starNullRe.ReplaceAllStringFunc(body, func(match string) string {
				submatches := starNullRe.FindStringSubmatch(match)
				if len(submatches) >= 2 {
					key := submatches[1]
					lowerKey := strings.ToLower(key)
					if strings.Contains(lowerKey, "orderinfo") || strings.Contains(lowerKey, "order_info") {
						return fmt.Sprintf(`"%s":{}`, key)
					}
					return fmt.Sprintf(`"%s":{"starOrderInfo":{},"star_order_info":{}}`, key)
				}
				return match
			})
			ctx.Response.SetBody(body)
		}
	})
	go router.Run()
	defer router.Stop()

	// 导航到文章发布页
	log.Info("Navigating to article publish page...")
	if err := a.page.Navigate(configs.PublishArticle); err != nil {
		return fmt.Errorf("failed to navigate: %w", err)
	}
	if err := a.page.WaitLoad(); err != nil {
		return fmt.Errorf("failed to wait load: %w", err)
	}
	time.Sleep(3 * time.Second)

	// 检查并处理登录状态 (包含扫码等待)
	if err := EnsureLogin(a.page, a.cookieStore); err != nil {
		return err
	}

	// 重新导航到发布页以确保页面处于已登录下的正确渲染
	if err := a.page.Navigate(configs.PublishArticle); err != nil {
		return fmt.Errorf("failed to navigate: %w", err)
	}
	_ = a.page.Timeout(15 * time.Second).WaitLoad()
	time.Sleep(3 * time.Second)

	// 输入标题
	if err := a.inputTitle(title); err != nil {
		return fmt.Errorf("failed to input title: %w", err)
	}
	time.Sleep(1 * time.Second)

	// 输入正文
	if err := a.inputContent(content, opts); err != nil {
		return fmt.Errorf("failed to input content: %w", err)
	}
	time.Sleep(1 * time.Second)

	// 验证内容是否真的写入了编辑器
	if err := a.verifyContent(); err != nil {
		log.Warnf("内容验证警告: %v", err)
	}

	// 解析正文中的所有本地插图
	blocks := parseContentBlocks(content)
	var inlineImages []string
	for _, b := range blocks {
		if b.Type == "image" && b.Value != "" {
			inlineImages = append(inlineImages, b.Value)
		}
	}

	// 封面决策逻辑
	var targetCoverMode string
	var targetCovers []string
	var isAutoCover bool

	if opts != nil && opts.CoverImage != "" {
		// 1. CoverImage 是明确的封面意图，优先使用。
		targetCoverMode = "单图"
		targetCovers = []string{opts.CoverImage}
	} else if opts != nil && len(opts.Images) > 0 && len(inlineImages) == 0 {
		// 2. 没有 Markdown 内嵌图时，Images 兼容旧调用作为封面备选。
		if len(opts.Images) >= 3 {
			targetCoverMode = "三图"
			targetCovers = opts.Images[:3]
		} else {
			targetCoverMode = "单图"
			targetCovers = []string{opts.Images[0]}
		}
	} else {
		// 3. 有 Markdown 内嵌图时，Images 属于正文配图来源，封面从正文图片自适应提取，避免重复上传同一批图。
		if len(inlineImages) >= 3 {
			targetCoverMode = "三图"
			targetCovers = inlineImages[:3]
			isAutoCover = true
		} else if len(inlineImages) > 0 {
			targetCoverMode = "单图"
			targetCovers = []string{inlineImages[0]}
			isAutoCover = true
		} else {
			targetCoverMode = "无封面"
		}
	}

	// 设置封面模式并上传图片
	log.Infof("封面模式决策结果: 模式=%s, 封面图数=%d, 自适应=%t", targetCoverMode, len(targetCovers), isAutoCover)
	if err := a.setCoverMode(targetCoverMode); err != nil {
		log.Warnf("Failed to set cover mode to %s: %v", targetCoverMode, err)
	} else if len(targetCovers) > 0 {
		// 在自适应提取模式下，编辑器切换封面模式后需要短暂时间从正文同步图片并渲染封面槽，这里等待 3 秒以防重复上传
		if isAutoCover {
			log.Info("自适应提取封面模式，等待 3 秒让编辑器同步正文图片...")
			time.Sleep(3 * time.Second)
		}
		if err := a.uploadCovers(targetCovers, isAutoCover); err != nil {
			log.Warnf("Failed to upload cover images for mode %s: %v", targetCoverMode, err)
			if degradeErr := a.setCoverMode("无封面"); degradeErr != nil {
				log.Warnf("封面上传失败后降级为无封面也失败: %v", degradeErr)
			} else {
				log.Warn("封面上传失败，已降级为无封面以避免发布按钮被封面校验拦截")
			}
		}
	}
	if err := a.clearBlockingCoverWarning(); err != nil {
		log.Warnf("处理封面校验警告失败: %v", err)
	}

	// 设置原创标记
	if opts != nil && opts.Original {
		a.setOriginal()
	}

	// 设置虚构声明标记
	if opts != nil && opts.Fiction {
		a.setFictionDeclaration()
	}

	time.Sleep(1 * time.Second)

	// 点击发布或保存为草稿
	if opts != nil && opts.SaveAsDraft {
		log.Info("检测到 SaveAsDraft 为 true，等待自动保存草稿并退出...")
		time.Sleep(8 * time.Second)
	} else {
		if err := a.clickPublish(opts); err != nil {
			return fmt.Errorf("failed to publish: %w", err)
		}
	}

	if opts != nil && opts.SaveAsDraft {
		log.Info("Article draft save browser flow completed")
	} else {
		log.Info("Article publish browser flow completed, waiting for service-level verification")
	}
	return nil
}

func (a *ArticlePublishAction) inputTitle(title string) error {
	el, sel, err := findElement(a.page, 3*time.Second, ArticleTitleSelectors)
	if err != nil {
		return fmt.Errorf("title input not found: %w", err)
	}
	log.Infof("Found title input using selector: %s", sel)

	// 如果标题超出了头条 30 字的限制，自动进行截断以防止被前端表单验证拦截导致发布按钮锁死
	if utf8.RuneCountInString(title) > 30 {
		runes := []rune(title)
		title = string(runes[:30])
		log.Warnf("【标题限制】文章标题超出了今日头条 30 字上限，已自动截断为: %s", title)
	}

	return inputTextWithFallback(el, title)
}

// ContentBlock 内容块，区分文本和图片
type ContentBlock struct {
	Type  string // "text" 或 "image"
	Value string // 文本内容或本地图片绝对路径
}

// parseContentBlocks 解析正文中的 Markdown 图片，分割成文本和图片块
func parseContentBlocks(content string) []ContentBlock {
	re := regexp.MustCompile(`!\[.*?\]\((.*?)\)`)
	indices := re.FindAllStringIndex(content, -1)
	matches := re.FindAllStringSubmatch(content, -1)

	var blocks []ContentBlock
	lastIdx := 0

	for i, idx := range indices {
		start := idx[0]
		end := idx[1]

		// 前面的文本块
		if start > lastIdx {
			blocks = append(blocks, ContentBlock{
				Type:  "text",
				Value: content[lastIdx:start],
			})
		}

		// 图片块
		imgPath := matches[i][1]
		blocks = append(blocks, ContentBlock{
			Type:  "image",
			Value: imgPath,
		})

		lastIdx = end
	}

	// 剩余文本块
	if lastIdx < len(content) {
		blocks = append(blocks, ContentBlock{
			Type:  "text",
			Value: content[lastIdx:],
		})
	}

	return blocks
}

// resolveImagePath 解析正文中类似 IMG_PLACEHOLDER_X 占位符路径，映射为 opts.Images 中对应的真实绝对路径。
// 如果不匹配，则返回原路径。
func resolveImagePath(path string, opts *ArticleOptions) string {
	if opts == nil || len(opts.Images) == 0 {
		return path
	}
	// 匹配 IMG_PLACEHOLDER_X (X 从 1 开始)
	re := regexp.MustCompile(`(?i)IMG_PLACEHOLDER_(\d+)`)
	matches := re.FindStringSubmatch(path)
	if len(matches) == 2 {
		var idx int
		_, err := fmt.Sscanf(matches[1], "%d", &idx)
		if err == nil && idx >= 1 && idx <= len(opts.Images) {
			// 返回对应的物理绝对路径
			return opts.Images[idx-1]
		}
	}
	return path
}

func (a *ArticlePublishAction) inputContent(content string, opts *ArticleOptions) error {
	el, sel, err := findElement(a.page, 3*time.Second, ArticleContentSelectors)
	if err != nil {
		return fmt.Errorf("content editor not found: %w", err)
	}
	log.Infof("Found content editor using selector: %s", sel)

	// 解析内容块
	blocks := parseContentBlocks(content)

	log.Infof("准备插入正文，共包含 %d 个文本/插图内容块...", len(blocks))

	// 先清空编辑器并 focus (优先使用 ProseMirror API 彻底同步清空其内部 State，防止残留)
	_, err = el.Eval(`() => {
		this.focus();
		
		let view = null;
		if (this.pmViewDesc && this.pmViewDesc.view) {
			view = this.pmViewDesc.view;
		} else if (this.cmView && this.cmView.view) {
			view = this.cmView.view;
		}

		if (view && view.state && view.dispatch) {
			try {
				const { state } = view;
				const schema = state.schema;
				const newDoc = schema.nodes.doc.create(null, [schema.nodes.paragraph.create()]);
				const tr = state.tr.replaceWith(0, state.doc.content.size, newDoc.content);
				view.dispatch(tr);
				view.focus();
				return;
			} catch(e) {
				console.warn('ProseMirror clear failed:', e);
			}
		}

		// 兜底原生 DOM 与编辑指令清空
		this.innerHTML = '';
		if (document.execCommand) {
			document.execCommand('selectAll', false, null);
			document.execCommand('delete', false, null);
		}
		this.dispatchEvent(new Event('input', {bubbles: true}));
		this.dispatchEvent(new Event('change', {bubbles: true}));
	}`)
	if err != nil {
		log.Warnf("清空编辑器失败: %v", err)
	}
	time.Sleep(1 * time.Second)

	for i, b := range blocks {
		if b.Type == "text" {
			log.Infof("正在插入文本块 %d/%d...", i+1, len(blocks))
			if err := a.insertTextAtCursor(el, b.Value); err != nil {
				return fmt.Errorf("插入文本块失败: %w", err)
			}
			time.Sleep(500 * time.Millisecond)
		} else {
			realPath := resolveImagePath(b.Value, opts)
			log.Infof("正在插入图片块 %d/%d: %s", i+1, len(blocks), realPath)
			if err := a.insertImageAtCursor(realPath); err != nil {
				return fmt.Errorf("插入图片块失败: %w", err)
			}
			// 插入图片后将光标强制定位在编辑器最后
			a.focusEditorEnd(el)
			time.Sleep(1500 * time.Millisecond) // 等待图片渲染
		}
	}

	return nil
}

// insertTextAtCursor 通过 ProseMirror View API 渲染富文本或 execCommand 兜底在当前光标处追加文本
func (a *ArticlePublishAction) insertTextAtCursor(el *rod.Element, text string) error {
	_, err := el.Eval(`(text) => {
		this.focus();

		// 1. 简易 Markdown 转 HTML 渲染器，用以将 ###/####/** 等标记清洗并转换为正确的富文本 HTML 节点
		const mdToHtml = (md) => {
			// 进行 HTML 实体编码防注入，但需保留我们要解析出的 HTML
			let html = md.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
			
			// 处理所有层级的标题 (### / #### 等) 并自动洗掉标题内的加粗 ** 符号
			html = html.replace(/^\s*(#{1,6})\s+(.*?)\s*#*\s*$/gm, (match, hashes, content) => {
				const level = hashes.length;
				let cleanContent = content.replace(/\*\*/g, '').replace(/__/g, '');
				return '<h' + level + '>' + cleanContent + '</h' + level + '>';
			});

			// 转换粗体 **text** 或 __text__ -> <strong>text</strong>
			html = html.replace(/\*\*(.*?)\*\*/g, '<strong>$1</strong>');
			html = html.replace(/__(.*?)__/g, '<strong>$1</strong>');

			// 转换斜体 *text* 或 _text_ -> <em>text</em>
			html = html.replace(/\*(.*?)\*/g, '<em>$1</em>');
			html = html.replace(/_(.*?)_/g, '<em>$1</em>');

			// 将无序列表项 - 或是 * 转换为 <li> 标签，保留内容
			html = html.replace(/^\s*[-*+]\s+(.*)$/gm, '<li>$1</li>');

			// 按行切分，对普通文字包裹 <p>，空行包裹 <p><br></p>，并拼装 <ul> 块
			let lines = html.split('\n');
			let inList = false;
			let formatted = [];
			
			for (let i = 0; i < lines.length; i++) {
				let line = lines[i].trim();
				if (line === '') {
					if (inList) {
						formatted.push('</ul>');
						inList = false;
					}
					formatted.push('<p><br></p>');
					continue;
				}
				
				// 判断标题
				if (line.startsWith('<h1') || line.startsWith('<h2') || line.startsWith('<h3') || line.startsWith('<h4') || line.startsWith('<h5') || line.startsWith('<h6')) {
					if (inList) {
						formatted.push('</ul>');
						inList = false;
					}
					formatted.push(line);
					continue;
				}
				
				// 判断列表项
				if (line.startsWith('<li>')) {
					if (!inList) {
						formatted.push('<ul>');
						inList = true;
					}
					formatted.push(line);
					continue;
				}
				
				// 列表项结束
				if (inList) {
					formatted.push('</ul>');
					inList = false;
				}
				
				formatted.push('<p>' + line + '</p>');
			}
			
			if (inList) {
				formatted.push('</ul>');
			}
			
			return formatted.join('');
		};

		// 2. 简易 Markdown 转纯文本渲染器，抹除所有标记符号，防止字符残留
		const mdToCleanText = (md) => {
			let clean = md;
			// 移除所有层级标题前面的 # 标记，例如 #### 一、标题 -> 一、标题
			clean = clean.replace(/^\s*#+\s+/gm, '');
			// 移除粗体标记
			clean = clean.replace(/\*\*/g, '').replace(/__/g, '');
			// 移除斜体标记
			clean = clean.replace(/\*/g, '').replace(/_/g, '');
			// 移除无序列表项行首的 -
			clean = clean.replace(/^\s*[-*+]\s+/gm, '');
			return clean;
		};

		let view = null;
		if (this.pmViewDesc && this.pmViewDesc.view) {
			view = this.pmViewDesc.view;
		} else if (this.cmView && this.cmView.view) {
			view = this.cmView.view;
		} else {
			let p = this;
			for (let i = 0; i < 5; i++) {
				if (p.pmViewDesc && p.pmViewDesc.view) {
					view = p.pmViewDesc.view;
					break;
				}
				p = p.parentElement;
				if (!p) break;
			}
		}

		if (view && view.state && view.dispatch) {
			try {
				const { state } = view;
				const tempDiv = document.createElement('div');
				tempDiv.innerHTML = mdToHtml(text);
				
				const domParser = view.domParser || view.state.schema.cached.domParser;
				const slice = domParser.parseSlice(tempDiv);
				const tr = state.tr.replaceSelection(slice);
				view.dispatch(tr);
				view.focus();
				console.log("Successfully inserted formatted HTML via ProseMirror DOM parser");
				return true;
			} catch (e) {
				console.warn('ProseMirror HTML slice insert failed, fallback to tr.insertText:', e);
				try {
					const { state } = view;
					const { selection } = state;
					const tr = state.tr;
					// 即使是 tr.insertText，我们也做文本清洗，确保无 markdown 残留
					tr.insertText(mdToCleanText(text), selection.from, selection.to);
					view.dispatch(tr);
					view.focus();
					console.log("Successfully inserted clean text via ProseMirror tr.insertText");
					return true;
				} catch (e2) {
					console.error("ProseMirror text insert fallback failed:", e2);
				}
			}
		}

		// 兜底：如果不是 ProseMirror 或 API 执行异常，执行原生 execCommand
		console.log("Falling back to document.execCommand for inserting text/HTML");
		if (document.execCommand) {
			try {
				// 优先尝试插入转换后的富文本 HTML
				console.log("Attempting document.execCommand('insertHTML')");
				const htmlContent = mdToHtml(text);
				const success = document.execCommand('insertHTML', false, htmlContent);
				if (success) {
					console.log("Successfully inserted HTML via execCommand('insertHTML')");
					return true;
				}
			} catch (errHTML) {
				console.warn("execCommand('insertHTML') failed, fallback to 'insertText':", errHTML);
			}

			try {
				console.log("Attempting document.execCommand('insertText') with clean text");
				document.execCommand('insertText', false, mdToCleanText(text));
				return true;
			} catch (errText) {
				console.error("execCommand('insertText') failed:", errText);
			}
		}

		// 最后的硬兜底
		console.log("Final fallback to textContent modification with clean text");
		this.textContent += mdToCleanText(text);
		this.dispatchEvent(new Event('input', { bubbles: true }));
		return false;
	}`, text)
	return err
}

// focusEditorEnd 将光标定位至编辑器末尾，支持 ProseMirror API 并提供 DOM Selection 兜底
func (a *ArticlePublishAction) focusEditorEnd(el *rod.Element) {
	_, _ = el.Eval(`() => {
		this.focus();
		let view = null;
		if (this.pmViewDesc && this.pmViewDesc.view) {
			view = this.pmViewDesc.view;
		} else if (this.cmView && this.cmView.view) {
			view = this.cmView.view;
		} else {
			let p = this;
			for (let i = 0; i < 5; i++) {
				if (p.pmViewDesc && p.pmViewDesc.view) {
					view = p.pmViewDesc.view;
					break;
				}
				p = p.parentElement;
				if (!p) break;
			}
		}

		if (view && view.state && view.dispatch) {
			try {
				const { state } = view;
				const tr = state.tr;
				const SelectionClass = state.selection.constructor;
				const $pos = state.doc.resolve(state.doc.content.size);
				const sel = SelectionClass.near($pos);
				tr.setSelection(sel);
				tr.scrollIntoView();
				view.dispatch(tr);
				view.focus();
				console.log("Successfully focused at end via ProseMirror view");
				return;
			} catch(e) {
				console.warn("ProseMirror focusEnd failed:", e);
			}
		}

		// 兜底 DOM selection
		try {
			let range = document.createRange();
			range.selectNodeContents(this);
			range.collapse(false); // 折叠至末尾
			let sel = window.getSelection();
			sel.removeAllRanges();
			sel.addRange(range);
		} catch(e) {
			console.error("DOM focus end failed:", e);
		}
	}`)
}

// insertImageAtCursor 在编辑器当前光标处插入图片，模拟点击工具栏并上传
func (a *ArticlePublishAction) insertImageAtCursor(imagePath string) error {
	absPath, cleanup, err := downloadImageToTemp(imagePath)
	if err != nil {
		return fmt.Errorf("准备图片文件失败 (%s): %w", imagePath, err)
	}
	defer cleanup()
	if stat, statErr := os.Stat(absPath); statErr != nil {
		return fmt.Errorf("正文图片文件不可读 (%s): %w", absPath, statErr)
	} else if stat.Size() <= 0 {
		return fmt.Errorf("正文图片文件为空: %s", absPath)
	} else {
		log.Infof("正文图片已准备完成: %s, size=%d bytes", absPath, stat.Size())
	}
	log.Infof("开始在编辑器中插入图片: %s (绝对路径: %s)", imagePath, absPath)

	// 1. 隐藏可能造成遮挡的页面元素
	a.dismissObstacles()

	// 清理临时标记
	_, _ = a.page.Eval(`() => {
		document.querySelectorAll('.mcp-toolbar-img-click').forEach(el => el.classList.remove('mcp-toolbar-img-click'));
	}`)

	// 2. 查找富文本编辑器顶部的“图片”按钮。
	imgBtnRes, err := a.page.Timeout(5 * time.Second).Eval(`() => {` + SafeScrollJS + `
		let tool = document.querySelector('div.syl-toolbar-tool.image') || 
		           document.querySelector('[class*="syl-toolbar-tool"][class*="image"]') ||
		           Array.from(document.querySelectorAll('.syl-toolbar-button')).find(btn => {
		               let parent = btn.closest('[class*="image"]');
		               return parent !== null;
		           });
		
		if (tool) {
			let btn = tool.tagName === 'BUTTON' ? tool : tool.querySelector('button');
			if (btn) {
				scrollIntoViewSafe(btn);
				btn.classList.add('mcp-toolbar-img-click');
				return true;
			}
		}
		return false;
	}`)

	if err != nil || (imgBtnRes != nil && !imgBtnRes.Value.Bool()) {
		return fmt.Errorf("未在编辑器工具栏中找到图片按钮: %v", err)
	}

	// 在 Go 层面物理点击
	clickBtn, err := a.page.Timeout(3 * time.Second).Element(".mcp-toolbar-img-click")
	if err != nil {
		return fmt.Errorf("定位临时标记的图片按钮失败: %w", err)
	}

	pt, err := clickBtn.Interactable()
	if err == nil {
		log.Infof("物理点击编辑器图片按钮，坐标 (%f, %f)", pt.X, pt.Y)
		_ = a.page.Mouse.MoveTo(*pt)
		_ = a.page.Mouse.Down(proto.InputMouseButtonLeft, 1)
		_ = a.page.Mouse.Up(proto.InputMouseButtonLeft, 1)
	} else {
		log.Warnf("图片按钮无法获取可交互坐标，回退到 JS 点击: %v", err)
		_, _ = clickBtn.Eval("() => this.click()")
	}
	time.Sleep(1500 * time.Millisecond)

	// 清理临时标记
	_, _ = a.page.Eval(`() => {
		document.querySelectorAll('.mcp-toolbar-img-click').forEach(el => el.classList.remove('mcp-toolbar-img-click'));
	}`)

	// 3. 通过 Chrome 原生文件选择器流程上传，避免直接 SetFiles 导致平台收到空文件数据。
	if err := a.uploadFileThroughChooser(absPath, "mcp-article-local-upload-trigger"); err != nil {
		safeScreenshot(a.page, "screenshot_article_image_upload_error.png")
		return fmt.Errorf("正文图片文件选择上传失败: %w", err)
	}
	time.Sleep(2 * time.Second)

	// 4. 等待确认弹窗并点击确认按钮
	log.Info("等待图片上传完毕并寻找确认按钮...")
	var clickedConfirm bool
	var dialogClosed bool
	for k := 0; k < 120; k++ { // 最多等约 60 秒以防大图片上传慢
		// 先清理旧标记
		_, _ = a.page.Eval(`() => {
			document.querySelectorAll('.mcp-confirm-btn').forEach(el => el.classList.remove('mcp-confirm-btn'));
		}`)

		// 检查确定按钮是否已启用（支持 button, div, span, a），只有在非禁用状态下才会被标记并返回 ready: true
		resConfirm, errEval := a.page.Eval(`() => {
			let btn = document.querySelector('[data-e2e="imageUploadConfirm-btn"]') ||
			          Array.from(document.querySelectorAll('button, div, span, a')).find(b => {
			              let text = b.textContent ? b.textContent.trim() : '';
			              return (text === '确定' || text === '确认') && b.offsetWidth > 0;
			          });
			if (btn) {
				let isDisabled = btn.disabled || 
				                 btn.classList.contains('is-disabled') || 
				                 btn.classList.contains('disabled') || 
				                 btn.className.includes('disabled') || 
				                 btn.getAttribute('disabled') !== null ||
				                 btn.classList.contains('semi-button-disabled') ||
				                 btn.classList.contains('byte-btn-disabled') ||
				                 btn.classList.contains('semi-button-disabled-primary') ||
				                 btn.classList.contains('semi-button-primary-disabled');
				if (!isDisabled) {
					btn.classList.add('mcp-confirm-btn');
					return { ready: true, foundBtn: true };
				}
				return { ready: false, foundBtn: true };
			}
			return { ready: false, foundBtn: false };
		}`)

		if errEval == nil && resConfirm != nil {
			readyVal := resConfirm.Value.Get("ready")
			foundBtnVal := resConfirm.Value.Get("foundBtn")

			if readyVal.Val() != nil && readyVal.Bool() {
				// 确定按钮已启用，执行点击！
				confirmEl, errEl := a.page.Timeout(2 * time.Second).Element(".mcp-confirm-btn")
				if errEl == nil && confirmEl != nil {
					ptConfirm, errPt := confirmEl.Interactable()
					if errPt == nil {
						log.Infof("物理点击正文图片弹窗确认按钮，坐标 (%f, %f)", ptConfirm.X, ptConfirm.Y)
						_ = a.page.Mouse.MoveTo(*ptConfirm)
						_ = a.page.Mouse.Down(proto.InputMouseButtonLeft, 1)
						_ = a.page.Mouse.Up(proto.InputMouseButtonLeft, 1)
					} else {
						log.Warnf("确认按钮不可物理点击，回退到 JS 点击: %v", errPt)
						_, _ = confirmEl.Eval("() => this.click()")
					}
					clickedConfirm = true
				}
			} else if foundBtnVal.Val() != nil && !readyVal.Bool() {
				// 找到了确定按钮但处于禁用状态，尝试选中已上传的缩略图以尝试激活它
				_, _ = a.page.Eval(`() => {
					let imgs = Array.from(document.querySelectorAll('img'));
					imgs.forEach(img => {
						if (img.offsetWidth > 0 && img.offsetHeight > 0) {
							if (img.closest('[class*="header"]') || img.closest('[class*="sidebar"]') || img.closest('[class*="nav"]') || img.closest('[class*="user"]')) {
								return;
							}
							let wrapper = img.closest('[class*="item"]') || img.closest('[class*="card"]') || img;
							if (!wrapper.dataset.mcpClicked) {
								wrapper.click();
								wrapper.dataset.mcpClicked = "true";
							}
						}
					});
				}`)
			}
		}

		// 清理标记
		_, _ = a.page.Eval(`() => {
			document.querySelectorAll('.mcp-confirm-btn').forEach(el => el.classList.remove('mcp-confirm-btn'));
		}`)

		if clickedConfirm {
			// 等待弹窗在页面上关闭
			log.Info("等待正文图片上传确认弹窗关闭...")
			for j := 0; j < 20; j++ { // 最多等 10 秒让弹窗关闭
				resExist, errExist := a.page.Eval(`() => {
					const visible = (el) => {
						if (!el) return false;
						const style = getComputedStyle(el);
						return el.offsetWidth > 0 && el.offsetHeight > 0 && style.display !== 'none' && style.visibility !== 'hidden';
					};
					const roots = Array.from(document.querySelectorAll('.upload-image-panel, .byte-modal, .semi-modal, [role="dialog"], [class*="modal"], [class*="dialog"]')).filter(visible);
					for (const root of roots) {
						const btn = root.querySelector('[data-e2e="imageUploadConfirm-btn"]') ||
							Array.from(root.querySelectorAll('button, div, span, a')).find(b => {
								const text = b.textContent ? b.textContent.trim() : '';
								return (text === '确定' || text === '确认') && visible(b);
							});
						if (btn && visible(btn)) return true;
					}
					const globalBtn = document.querySelector('[data-e2e="imageUploadConfirm-btn"]');
					return visible(globalBtn);
				}`)
				if errExist == nil && resExist != nil && !resExist.Value.Bool() {
					log.Info("正文图片上传确认弹窗已成功关闭")
					dialogClosed = true
					break
				}
				time.Sleep(500 * time.Millisecond)
			}
			if dialogClosed {
				break
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	if !dialogClosed {
		log.Warnf("正文图片确认弹窗未能正常关闭，保存截图后中断本次发布以暴露上传失败原因")
		safeScreenshot(a.page, "screenshot_article_image_confirm_error.png")
		// 1. 在 JS 中尝试点击右上角的“关闭/X”按钮，或者“取消”按钮以关闭弹窗
		_, _ = a.page.Eval(`() => {
			let closeBtn = Array.from(document.querySelectorAll('button, div, span, a, i, svg')).find(el => {
				let text = el.textContent ? el.textContent.trim() : '';
				let cls = el.className ? String(el.className) : '';
				let isClose = (text === '取消' || text === '关闭' || cls.includes('close') || cls.includes('cancel') || el.querySelector('[class*="close"]') !== null);
				return isClose && el.offsetWidth > 0;
			});
			if (closeBtn) {
				closeBtn.click();
				return true;
			}
			return false;
		}`)
		// 2. 模拟 ESC 按键强退
		_, _ = a.page.Eval(`() => {
			window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', code: 'Escape', keyCode: 27, which: 27, bubbles: true }));
		}`)
		time.Sleep(1 * time.Second)
		return fmt.Errorf("正文图片上传确认失败，可能仍存在图片校验错误或上传面板未完成处理，已保存截图 screenshot_article_image_confirm_error.png")
	}

	return nil
}

// verifyContent 验证标题和正文是否真正写入
func (a *ArticlePublishAction) verifyContent() error {
	// 检查标题
	titleEl, _, err := findElement(a.page, 2*time.Second, ArticleTitleSelectors)
	if err == nil && titleEl != nil {
		titleVal, _ := titleEl.Eval(`() => this.value || this.innerText || ''`)
		if titleVal != nil {
			titleText := strings.TrimSpace(titleVal.Value.Str())
			if titleText == "" {
				return fmt.Errorf("标题输入后验证为空，可能未成功写入")
			}
			log.Infof("标题验证通过，内容: %s", truncateStr(titleText, 30))
		}
	}

	// 检查正文编辑器
	contentEl, _, err := findElement(a.page, 2*time.Second, ArticleContentSelectors)
	if err == nil && contentEl != nil {
		contentVal, _ := contentEl.Eval(`() => this.innerText || this.textContent || ''`)
		if contentVal != nil {
			contentText := strings.TrimSpace(contentVal.Value.Str())
			if contentText == "" {
				return fmt.Errorf("正文输入后验证为空，可能未成功写入编辑器")
			}
			log.Infof("正文验证通过，长度: %d 字符", len([]rune(contentText)))
		}
	}

	return nil
}

// dismissObstacles 隐藏可能遮挡页面交互元素的浮动遮罩和侧边栏（如AI写作助手）
func (a *ArticlePublishAction) dismissObstacles() {
	_, _ = a.page.Eval(`() => {
		// 隐藏可能覆盖按钮的遮罩层、顶栏以及预览遮罩层
		document.querySelectorAll('.byte-drawer-mask, .semi-drawer-mask, [class*="drawer-mask"], [class*="mask"], .preview-article-mask, .shead_wrap, [class*="shead_wrap"]').forEach(el => {
			el.style.pointerEvents = 'none';
			el.style.display = 'none';
		});
		// 隐藏抽屉本身（例如右侧的AI创作助手抽屉）
		document.querySelectorAll('.byte-drawer, .semi-drawer, [class*="drawer"]').forEach(el => {
			el.style.pointerEvents = 'none';
			el.style.display = 'none';
		});
	}`)
}

func (a *ArticlePublishAction) uploadFileThroughChooser(localPath, markerClass string) error {
	setFiles, err := a.page.HandleFileDialog()
	if err != nil {
		return fmt.Errorf("初始化 Chrome 文件选择器拦截失败: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- setFiles([]string{localPath})
	}()

	_, _ = a.page.Eval(`(markerClass) => {
		document.querySelectorAll('.' + markerClass).forEach(el => el.classList.remove(markerClass));
		const visible = (el) => {
			if (!el) return false;
			const style = window.getComputedStyle(el);
			const rect = el.getBoundingClientRect();
			return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
		};
		const panels = Array.from(document.querySelectorAll('.upload-image-panel, .byte-modal, .semi-modal, [role="dialog"], [class*="modal"], [class*="drawer"]'))
			.filter(visible);
		for (const panel of panels) {
			const controls = Array.from(panel.querySelectorAll('button, [role="button"], label, .upload-handler, input[type="file"]')).filter(visible);
			let trigger = controls.find(el => (el.textContent || '').trim().includes('本地上传'));
			if (!trigger) {
				const input = panel.querySelector('input[type="file"]');
				if (input) trigger = input.closest('button, label, [role="button"], .upload-handler') || input;
			}
			if (trigger) {
				trigger.classList.add(markerClass);
				return true;
			}
		}
		return false;
	}`, markerClass)

	trigger, err := a.page.Timeout(5 * time.Second).Element("." + markerClass)
	if err != nil {
		_ = proto.PageSetInterceptFileChooserDialog{Enabled: false}.Call(a.page)
		return fmt.Errorf("未找到当前上传面板的“本地上传”触发按钮: %w", err)
	}
	trigger = trigger.CancelTimeout()

	pt, err := trigger.Interactable()
	if err == nil {
		log.Infof("通过 Chrome 文件选择器流程点击上传触发器，坐标 (%f, %f)", pt.X, pt.Y)
		_ = a.page.Mouse.MoveTo(*pt)
		_ = a.page.Mouse.Down(proto.InputMouseButtonLeft, 1)
		_ = a.page.Mouse.Up(proto.InputMouseButtonLeft, 1)
	} else {
		log.Warnf("上传触发器不可物理点击，回退到 JS 事件链: %v", err)
		_, _ = trigger.Eval(`() => {
			const input = this.matches('input[type="file"]') ? this : this.querySelector('input[type="file"]');
			const target = input || this;
			['pointerdown', 'mousedown', 'pointerup', 'mouseup', 'click'].forEach(name => {
				target.dispatchEvent(new MouseEvent(name, { bubbles: true, cancelable: true, view: window }));
			});
		}`)
	}

	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("Chrome 文件选择器写入文件失败: %w", err)
		}
		log.Infof("Chrome 文件选择器已接收文件: %s", localPath)
		return nil
	case <-time.After(8 * time.Second):
		_ = proto.PageSetInterceptFileChooserDialog{Enabled: false}.Call(a.page)
		return fmt.Errorf("等待 Chrome 文件选择器弹出超时")
	}
}

func (a *ArticlePublishAction) uploadCovers(coverPaths []string, isAutoCover bool) error {
	log.Infof("Uploading %d cover images...", len(coverPaths))

	// 确保展开“发文设置”以露出封面插槽
	ensureSettingsExpanded(a.page)

	var cleanups []func()
	defer func() {
		for _, cleanup := range cleanups {
			cleanup()
		}
	}()

	for i, path := range coverPaths {
		// 头条会从正文图片自动同步封面槽。若槽位已有图，继续点“编辑/替换”很容易进入错误的弹层或触发重复封面校验。
		hasImg, errCheck := a.checkCoverSlotHasImage(i)
		if errCheck == nil && hasImg {
			modeNote := "显式封面"
			if isAutoCover {
				modeNote = "自适应封面"
			}
			log.Infof("检测到封面槽 %d 已有图片（%s），跳过重复上传", i+1, modeNote)
			continue
		}

		localPath, cleanup, err := downloadImageToTemp(path)
		if err != nil {
			return fmt.Errorf("准备封面图片失败 (%s): %w", path, err)
		}
		cleanups = append(cleanups, cleanup)
		if stat, statErr := os.Stat(localPath); statErr != nil {
			return fmt.Errorf("封面图片文件不可读 (%s): %w", localPath, statErr)
		} else if stat.Size() <= 0 {
			return fmt.Errorf("封面图片文件为空: %s", localPath)
		} else {
			log.Infof("封面图片已准备完成: %s, size=%d bytes", localPath, stat.Size())
		}

		log.Infof("Uploading cover %d: %s (本地路径: %s)", i+1, path, localPath)

		// 隐藏可能造成遮挡的页面元素
		a.dismissObstacles()

		// 先清理已有的标记类名
		_, _ = a.page.Eval(`() => {
			document.querySelectorAll('.mcp-target-to-click').forEach(el => el.classList.remove('mcp-target-to-click'));
		}`)

		// 每次上传前重新获取“空封面槽”，避免宽泛选择器命中外层容器后重复点击同一张封面。
		coverEls, err := a.page.Timeout(3 * time.Second).Elements(`div.article-cover-add`)
		if err != nil || len(coverEls) == 0 {
			log.Warnf("Failed to find exact cover add slots, trying fallback selectors...")
			coverEl, _, err := findElement(a.page, 3*time.Second, ArticleCoverAddSelectors)
			if err != nil {
				return fmt.Errorf("cover area not found for image %d: %w", i+1, err)
			}
			coverEls = rod.Elements{coverEl}
		}

		if len(coverEls) == 0 {
			return fmt.Errorf("no cover upload slots found for image %d", i+1)
		}

		var targetEl *rod.Element
		if i < len(coverEls) {
			targetEl = coverEls[i]
		} else if len(coverEls) > 0 {
			targetEl = coverEls[len(coverEls)-1]
		} else {
			return fmt.Errorf("no available empty cover upload slot for image %d", i+1)
		}

		// 滚动到中间并使用 scrollIntoViewSafe，标记 class
		_, _ = targetEl.Eval(`() => {` + SafeScrollJS + `
			scrollIntoViewSafe(this);
			this.classList.add('mcp-target-to-click');
		}`)
		time.Sleep(500 * time.Millisecond)

		// 在 Go 层面直接使用标记定位物理点击封面框
		clickEl, err := a.page.Timeout(3 * time.Second).Element(".mcp-target-to-click")
		if err != nil {
			return fmt.Errorf("could not locate marked cover upload slot for image %d: %w", i+1, err)
		}

		// 模拟物理鼠标点击；封面槽内部的隐藏 input 不再直接注入，必须先让页面打开真实上传面板。
		pt, err := clickEl.Interactable()
		if err == nil {
			log.Infof("Clicking cover slot %d at point (%f, %f)", i+1, pt.X, pt.Y)
			_ = a.page.Mouse.MoveTo(*pt)
			_ = a.page.Mouse.Down(proto.InputMouseButtonLeft, 1)
			_ = a.page.Mouse.Up(proto.InputMouseButtonLeft, 1)
		} else {
			log.Warnf("Failed to get interactable point for cover slot %d, fallback to JS full events click: %v", i+1, err)
			_, _ = clickEl.Eval(`() => {
				const events = ['pointerdown', 'mousedown', 'pointerup', 'mouseup', 'click'];
				events.forEach(name => {
					const ev = new MouseEvent(name, { bubbles: true, cancelable: true, view: window });
					this.dispatchEvent(ev);
				});
			}`)
		}

		// 延迟一小段时间，检查上传弹窗是否已经成功弹出
		hasDialog := false
		for d := 0; d < 6; d++ { // 等待最多 3 秒
			resDialog, errDialog := a.page.Eval(`() => {
				const visible = (el) => {
					if (!el) return false;
					const style = window.getComputedStyle(el);
					const rect = el.getBoundingClientRect();
					return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
				};
				return Array.from(document.querySelectorAll('input[type="file"]')).some(inp => {
					let parent = inp.closest('.upload-image-panel, .byte-modal, .semi-modal, [role="dialog"], [class*="modal"], [class*="dialog"], [class*="drawer"]');
					return parent !== null && visible(parent);
				});
			}`)
			if errDialog == nil && resDialog != nil && resDialog.Value.Bool() {
				hasDialog = true
				break
			}
			time.Sleep(500 * time.Millisecond)
		}

		if !hasDialog {
			log.Warnf("【封面上传】物理点击封面槽 %d 疑似未成功弹出上传窗口，执行 JS Click 强制兜底...", i+1)
			_, _ = clickEl.Eval("() => this.click()")
			time.Sleep(1500 * time.Millisecond)
		} else {
			time.Sleep(1000 * time.Millisecond)
		}

		// 清理临时标记
		_, _ = a.page.Eval(`() => {
			document.querySelectorAll('.mcp-target-to-click').forEach(el => el.classList.remove('mcp-target-to-click'));
		}`)

		if err := a.uploadFileThroughChooser(localPath, "mcp-cover-local-upload-trigger"); err != nil {
			safeScreenshot(a.page, "screenshot_upload_error.png")
			log.Warnf("Upload error screenshot saved to screenshot_upload_error.png")
			return fmt.Errorf("cover image %d Chrome 文件选择上传失败: %w", i+1, err)
		}
		time.Sleep(2 * time.Second)

		// 等待并点击图片确认按钮（如 data-e2e="imageUploadConfirm-btn" 或文本为“确定”的按钮）
		log.Infof("Waiting for image %d upload and confirm dialog...", i+1)
		var clickedImgConfirm bool
		var dialogClosed bool
		for k := 0; k < 120; k++ { // 最多等约 60 秒以防大图片上传慢
			// 先清理标记
			_, _ = a.page.Eval(`() => {
				document.querySelectorAll('.mcp-confirm-btn').forEach(el => el.classList.remove('mcp-confirm-btn'));
			}`)

			// 检查确定按钮是否已启用（支持 button, div, span, a），只有在非禁用状态下才会被标记并返回 ready: true
			resConfirm, errEval := a.page.Eval(`() => {
				let btn = document.querySelector('[data-e2e="imageUploadConfirm-btn"]') ||
				          Array.from(document.querySelectorAll('button, div, span, a')).find(b => {
				              let text = b.textContent ? b.textContent.trim() : '';
				              return (text === '确定' || text === '确认') && b.offsetWidth > 0;
				          });
				if (btn) {
					let isDisabled = btn.disabled || 
					                 btn.classList.contains('is-disabled') || 
					                 btn.classList.contains('disabled') || 
					                 btn.className.includes('disabled') || 
					                 btn.getAttribute('disabled') !== null ||
					                 btn.classList.contains('semi-button-disabled') ||
					                 btn.classList.contains('byte-btn-disabled') ||
					                 btn.classList.contains('semi-button-disabled-primary') ||
					                 btn.classList.contains('semi-button-primary-disabled');
					if (!isDisabled) {
						btn.classList.add('mcp-confirm-btn');
						return { ready: true, foundBtn: true };
					}
					return { ready: false, foundBtn: true };
				}
				return { ready: false, foundBtn: false };
			}`)

			if errEval == nil && resConfirm != nil {
				readyVal := resConfirm.Value.Get("ready")
				foundBtnVal := resConfirm.Value.Get("foundBtn")

				if readyVal.Val() != nil && readyVal.Bool() {
					// 确定按钮已启用，执行点击！
					confirmEl, errEl := a.page.Timeout(2 * time.Second).Element(".mcp-confirm-btn")
					if errEl == nil && confirmEl != nil {
						ptConfirm, errPt := confirmEl.Interactable()
						if errPt == nil {
							log.Infof("Physically clicking image upload confirm button for image %d at (%f, %f)", i+1, ptConfirm.X, ptConfirm.Y)
							_ = a.page.Mouse.MoveTo(*ptConfirm)
							_ = a.page.Mouse.Down(proto.InputMouseButtonLeft, 1)
							_ = a.page.Mouse.Up(proto.InputMouseButtonLeft, 1)
						} else {
							log.Warnf("Failed to get interactable point for confirm button, fallback to JS click: %v", errPt)
							_, _ = confirmEl.Eval("() => this.click()")
						}
						clickedImgConfirm = true
					}
				} else if foundBtnVal.Val() != nil && !readyVal.Bool() {
					// 找到了确定按钮但处于禁用状态，或者没找到，尝试选中已上传的缩略图以尝试激活它
					_, _ = a.page.Eval(`() => {
						let imgs = Array.from(document.querySelectorAll('img'));
						imgs.forEach(img => {
							if (img.offsetWidth > 0 && img.offsetHeight > 0) {
								if (img.closest('[class*="header"]') || img.closest('[class*="sidebar"]') || img.closest('[class*="nav"]') || img.closest('[class*="user"]')) {
									return;
								}
								let wrapper = img.closest('[class*="item"]') || img.closest('[class*="card"]') || img;
								if (!wrapper.dataset.mcpClicked) {
									wrapper.click();
									wrapper.dataset.mcpClicked = "true";
								}
							}
						});
					}`)
				}
			}

			// 清理标记
			_, _ = a.page.Eval(`() => {
				document.querySelectorAll('.mcp-confirm-btn').forEach(el => el.classList.remove('mcp-confirm-btn'));
			}`)

			if clickedImgConfirm {
				// 等待弹窗在页面上消失
				log.Info("Waiting for upload confirm dialog to close...")
				for j := 0; j < 20; j++ { // 最多等 10 秒让弹窗关闭
					resExist, errExist := a.page.Eval(`() => {
						const visible = (el) => {
							if (!el) return false;
							const style = getComputedStyle(el);
							return el.offsetWidth > 0 && el.offsetHeight > 0 && style.display !== 'none' && style.visibility !== 'hidden';
						};
						const roots = Array.from(document.querySelectorAll('.upload-image-panel, .byte-modal, .semi-modal, [role="dialog"], [class*="modal"], [class*="dialog"]')).filter(visible);
						for (const root of roots) {
							const btn = root.querySelector('[data-e2e="imageUploadConfirm-btn"]') ||
								Array.from(root.querySelectorAll('button, div, span, a')).find(b => {
									const text = b.textContent ? b.textContent.trim() : '';
									return (text === '确定' || text === '确认') && visible(b);
								});
							if (btn && visible(btn)) return true;
						}
						const globalBtn = document.querySelector('[data-e2e="imageUploadConfirm-btn"]');
						return visible(globalBtn);
					}`)
					if errExist == nil && resExist != nil && !resExist.Value.Bool() {
						log.Info("Upload confirm dialog closed successfully")
						dialogClosed = true
						break
					}
					time.Sleep(500 * time.Millisecond)
				}
				if dialogClosed {
					break
				}
			}
			time.Sleep(300 * time.Millisecond)
		}

		if !dialogClosed {
			log.Warnf("封面图片 %d 确认弹窗未能正常关闭，保存截图后中断本次发布以暴露上传失败原因", i+1)
			safeScreenshot(a.page, "screenshot_cover_upload_confirm_error.png")
			// 1. 在 JS 中尝试点击右上角的“关闭/X”按钮，或者“取消”按钮以关闭弹窗
			_, _ = a.page.Eval(`() => {
				let closeBtn = Array.from(document.querySelectorAll('button, div, span, a, i, svg')).find(el => {
					let text = el.textContent ? el.textContent.trim() : '';
					let cls = el.className ? String(el.className) : '';
					let isClose = (text === '取消' || text === '关闭' || cls.includes('close') || cls.includes('cancel') || el.querySelector('[class*="close"]') !== null);
					return isClose && el.offsetWidth > 0;
				});
				if (closeBtn) {
					closeBtn.click();
					return true;
				}
				return false;
			}`)
			// 2. 模拟 ESC 按键强退
			_, _ = a.page.Eval(`() => {
				window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', code: 'Escape', keyCode: 27, which: 27, bubbles: true }));
			}`)
			time.Sleep(1 * time.Second)
			return fmt.Errorf("封面图片 %d 上传确认失败，可能仍存在图片校验错误或上传面板未完成处理，已保存截图 screenshot_cover_upload_confirm_error.png", i+1)
		}

		time.Sleep(1500 * time.Millisecond)
	}
	return nil
}

func (a *ArticlePublishAction) clearBlockingCoverWarning() error {
	res, err := a.page.Eval(`() => {
		const text = document.body ? document.body.innerText || '' : '';
		return text.includes('请勿选择重复的封面') ||
			text.includes('重复的封面') ||
			text.includes('为保证读者体验');
	}`)
	if err != nil || res == nil || !res.Value.Bool() {
		return err
	}

	log.Warn("检测到封面重复/读者体验校验警告，自动切换为无封面以解除发布阻塞")
	if err := a.setCoverMode("无封面"); err != nil {
		return fmt.Errorf("切换无封面失败: %w", err)
	}
	time.Sleep(1 * time.Second)
	return nil
}

// checkCoverSlotHasImage 检查第 i 个封面插槽中是否已经有已上传的图片
func (a *ArticlePublishAction) checkCoverSlotHasImage(index int) (bool, error) {
	res, err := a.page.Timeout(3*time.Second).Eval(`(i) => {
		let radioGroup = document.querySelector('.article-cover-radio-group');
		let container = null;
		if (radioGroup) {
			container = radioGroup.closest('.byte-form-item, .semi-form-item, [class*="form-item"]') || radioGroup.parentElement.parentElement;
		} else {
			let label = Array.from(document.querySelectorAll('span, label, div')).find(el => {
				if (el.children.length > 0) return false;
				let text = el.textContent ? el.textContent.trim() : '';
				return text === '展示封面' || text === '* 展示封面' || text === '*展示封面';
			});
			if (label) {
				container = label.closest('.byte-form-item, .semi-form-item, [class*="form-item"]') || label.parentElement.parentElement;
			}
		}
		if (!container) {
			container = document.body;
		}

		let slots = Array.from(container.querySelectorAll('.article-cover-add, .article-cover-card, .article-cover-preview, [class*="cover-add"], [class*="cover-card"], [class*="cover-preview"], [class*="cover-item"]'));
		if (slots.length === 0) {
			slots = Array.from(container.querySelectorAll('div, label')).filter(el => {
				let className = el.className;
				if (typeof className !== 'string') return false;
				return (className.includes('cover') && (className.includes('item') || className.includes('card') || className.includes('add') || className.includes('img') || className.includes('preview')));
			});
		}

		if (slots.length > i) {
			let slot = slots[i];
			// 机制一：检测 img 元素及 src
			let img = slot.querySelector('img');
			if (img && img.src && img.src.trim() !== '') {
				return true;
			}
			if (slot.tagName === 'IMG' && slot.src && slot.src.trim() !== '') {
				return true;
			}

			// 机制二：检测 background-image 样式
			let style = slot.style.backgroundImage || slot.style.background || '';
			if (style.includes('url(')) {
				return true;
			}
			let hasBg = Array.from(slot.querySelectorAll('*')).some(el => {
				let s = el.style.backgroundImage || el.style.background || '';
				return s.includes('url(');
			});
			if (hasBg) {
				return true;
			}

			// 机制三：检测操作文本。当封面已被同步或上传后，槽中会出现“编辑/替换/修改/裁剪”的操作词
			let text = slot.textContent || '';
			if (text.includes('编辑') || text.includes('替换') || text.includes('修改') || text.includes('裁剪')) {
				return true;
			}
		}
		return false;
	}`, index)

	if err != nil {
		return false, err
	}
	if res != nil {
		return res.Value.Bool(), nil
	}
	return false, nil
}

func (a *ArticlePublishAction) setCoverMode(mode string) error {
	log.Infof("Attempting to select cover mode: %s", mode)

	// 1. 安全确保展开“发文设置”
	ensureSettingsExpanded(a.page)

	// 2. 清理已有标记和隐藏可能遮挡的元素
	a.dismissObstacles()
	_, _ = a.page.Eval(`() => {
		document.querySelectorAll('.mcp-target-to-click').forEach(el => el.classList.remove('mcp-target-to-click'));
	}`)

	// 3. JS 定位目标元素，滚动并打上临时标记
	res, err := a.page.Timeout(5*time.Second).Eval(`(modeText) => {`+SafeScrollJS+`
		let targetLabel = null;

		// 机制一：基于 .article-cover-radio-group 容器的直接子元素索引定位
		// 优先查找代表封面单选按钮组的容器，取其直接子元素
		let radioGroup = document.querySelector('.article-cover-radio-group');
		if (radioGroup) {
			let radios = Array.from(radioGroup.children).filter(el => {
				let text = el.textContent ? el.textContent.trim() : '';
				return text.includes('单图') || text.includes('三图') || text.includes('无封面');
			});
			if (radios.length === 3) {
				if (modeText === '单图') targetLabel = radios[0];
				else if (modeText === '三图') targetLabel = radios[1];
				else if (modeText === '无封面') targetLabel = radios[2];
			}
		}

		// 机制二：基于“展示封面”行容器的直接子元素索引定位
		if (!targetLabel) {
			let labelEl = Array.from(document.querySelectorAll('span, label, div')).find(el => {
				if (el.children.length > 0) return false;
				let text = el.textContent ? el.textContent.trim() : '';
				return text === '展示封面' || text === '* 展示封面' || text === '*展示封面';
			});
			if (labelEl) {
				let container = labelEl.closest('.byte-form-item, .semi-form-item, [class*="form-item"]') || labelEl.parentElement.parentElement;
				let subRadioGroup = container.querySelector('.article-cover-radio-group, [class*="radio-group"]');
				if (subRadioGroup) {
					let radios = Array.from(subRadioGroup.children).filter(el => {
						let text = el.textContent ? el.textContent.trim() : '';
						return text.includes('单图') || text.includes('三图') || text.includes('无封面');
					});
					if (radios.length === 3) {
						if (modeText === '单图') targetLabel = radios[0];
						else if (modeText === '三图') targetLabel = radios[1];
						else if (modeText === '无封面') targetLabel = radios[2];
					}
				}
			}
		}

		// 机制三：局部叶子单选框文本精准匹配
		if (!targetLabel) {
			let labelEl = Array.from(document.querySelectorAll('span, label, div')).find(el => {
				if (el.children.length > 0) return false;
				let text = el.textContent ? el.textContent.trim() : '';
				return text === '展示封面' || text === '* 展示封面' || text === '*展示封面';
			});
			if (labelEl) {
				let container = labelEl.closest('.byte-form-item, .semi-form-item, [class*="form-item"]') || labelEl.parentElement.parentElement;
				// 过滤出容器内的单选项，要求其内部不再含有任何单选 label 或 input 容器
				let radios = Array.from(container.querySelectorAll('.byte-radio, .semi-radio, label, [class*="radio"]')).filter(el => {
					return el.querySelectorAll('.byte-radio, .semi-radio, label, [class*="radio"]').length === 0;
				});
				targetLabel = radios.find(r => r.textContent && r.textContent.trim().includes(modeText));
			}
		}

		// 机制四：全局叶子单选框文本兜底
		if (!targetLabel) {
			let labels = Array.from(document.querySelectorAll('.byte-radio, .semi-radio, label, [class*="radio"]')).filter(el => {
				return el.querySelectorAll('.byte-radio, .semi-radio, label, [class*="radio"]').length === 0;
			});
			targetLabel = labels.find(l => l.textContent && l.textContent.trim().includes(modeText));
		}
		if (!targetLabel) {
			let span = Array.from(document.querySelectorAll('span')).find(el => el.textContent && el.textContent.trim() === modeText);
			if (span) {
				targetLabel = span.closest('label') || span;
			}
		}

		if (targetLabel) {
			scrollIntoViewSafe(targetLabel);
			targetLabel.classList.add('mcp-target-to-click');
			return JSON.stringify({
				found: true,
				html: targetLabel.outerHTML
			});
		}
		return JSON.stringify({
			found: false
		});
	}`, mode)

	if err != nil {
		return fmt.Errorf("failed to locate cover mode %s: %w", mode, err)
	}
	if res != nil {
		resultStr := res.Value.Str()
		log.Infof("Cover mode locating JS returned: %s", resultStr)
		if !strings.Contains(resultStr, `"found":true`) {
			return fmt.Errorf("cover mode %s option not found", mode)
		}
	}

	time.Sleep(500 * time.Millisecond)

	// 4. Go 层面物理点击以防触发 Go-rod 的二次滚动
	clickEl, err := a.page.Timeout(5 * time.Second).Element(".mcp-target-to-click")
	if err != nil {
		return fmt.Errorf("failed to get marked cover element: %w", err)
	}

	pt, err := clickEl.Interactable()
	if err != nil {
		return fmt.Errorf("failed to get interactable point for cover mode: %w", err)
	}

	log.Infof("Clicking cover mode '%s' at physical point (%f, %f)", mode, pt.X, pt.Y)

	if err := a.page.Mouse.MoveTo(*pt); err != nil {
		return fmt.Errorf("failed to move mouse to cover mode: %w", err)
	}
	if err := a.page.Mouse.Down(proto.InputMouseButtonLeft, 1); err != nil {
		return fmt.Errorf("failed to click down cover mode: %w", err)
	}
	if err := a.page.Mouse.Up(proto.InputMouseButtonLeft, 1); err != nil {
		return fmt.Errorf("failed to click up cover mode: %w", err)
	}

	// 5. 再次对 input 派发原生 change/click 事件双保险
	_, _ = a.page.Eval(`() => {
		let clickEl = document.querySelector('.mcp-target-to-click');
		if (clickEl) {
			let input = clickEl.querySelector('input[type="radio"]');
			if (!input) {
				input = clickEl.tagName === 'INPUT' ? clickEl : clickEl.querySelector('input');
			}
			if (input) {
				const nativeInputValueSetter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, "checked").set;
				nativeInputValueSetter.call(input, true);
				input.dispatchEvent(new Event('change', { bubbles: true }));
				input.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, view: window }));
			}
		}
	}`)

	// 6. 清理临时标记
	_, _ = a.page.Eval(`() => {
		document.querySelectorAll('.mcp-target-to-click').forEach(el => el.classList.remove('mcp-target-to-click'));
	}`)

	time.Sleep(1 * time.Second)
	return nil
}

func (a *ArticlePublishAction) setOriginal() {
	log.Info("Attempting to set '原创' label...")

	// 确保展开“发文设置”
	_, _ = a.page.Timeout(3 * time.Second).Eval(`() => {
		// 寻找任何可能代表原创、首发的相关选项。如果存在，说明抽屉原本就是展开的，直接退出，防止重复点击将其反向折叠！
		let originalFound = document.querySelector('.pgc-declare-original-checkbox') || 
		                    document.querySelector('input[name="original"]') || 
		                    document.querySelector('input[value="original"]') ||
		                    Array.from(document.querySelectorAll('span, label, p, div')).find(el => {
		                        let text = el.textContent ? el.textContent.trim() : '';
		                        return (text.includes('声明原创') || text.includes('首发') || text.includes('原创')) && el.offsetWidth > 0;
		                    });
		if (!originalFound) {
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

	// 清理已有的临时类名并隐藏障碍物
	a.dismissObstacles()

	// 1. JS 定位原创 checkbox 的包装元素，滚动并打上临时标记
	res, err := a.page.Timeout(5 * time.Second).Eval(`() => {` + SafeScrollJS + `
		let target = document.querySelector('.pgc-declare-original-checkbox') ||
		             document.querySelector('input[name="original"]') ||
		             document.querySelector('input[value="original"]') ||
		             document.querySelector('input[type="checkbox"][class*="original"]') ||
		             Array.from(document.querySelectorAll('.byte-checkbox, .semi-checkbox, label, span')).find(el => {
		                 let text = el.innerText ? el.innerText.trim() : (el.textContent ? el.textContent.trim() : '');
		                 return (text.includes('声明原创') || text.includes('原创') || text.includes('头条首发') || text.includes('首发')) && 
		                        (el.querySelector('input[type="checkbox"]') !== null || el.tagName === 'INPUT');
		             });
		if (target) {
			let clickTarget = target.tagName === 'INPUT' ? target.parentElement : target;
			if (clickTarget) {
				scrollIntoViewSafe(clickTarget);
				clickTarget.classList.add('mcp-target-to-click');
				return true;
			}
		}
		return false;
	}`)

	if err != nil || res == nil || !res.Value.Bool() {
		log.Warnf("JS定位原创Checkbox失败: %v", err)
		return
	}

	time.Sleep(500 * time.Millisecond)

	// 2. Go 物理点击标记元素
	clickEl, err := a.page.Timeout(3 * time.Second).Element(".mcp-target-to-click")
	physicalClicked := false
	if err == nil && clickEl != nil {
		pt, errPt := clickEl.Interactable()
		if errPt == nil {
			log.Infof("Physically clicking '原创/首发' checkbox wrapper at (%f, %f)", pt.X, pt.Y)
			_ = a.page.Mouse.MoveTo(*pt)
			_ = a.page.Mouse.Down(proto.InputMouseButtonLeft, 1)
			_ = a.page.Mouse.Up(proto.InputMouseButtonLeft, 1)
			physicalClicked = true
		} else {
			log.Warnf("Failed to get interactable point for original checkbox wrapper: %v", errPt)
		}
	}

	time.Sleep(1 * time.Second)

	// 3. 检查是否成功勾选（checked 是否为 true），否则执行 JS click 兜底
	resChecked, errCheck := a.page.Eval(`() => {
		let target = document.querySelector('.mcp-target-to-click');
		if (!target) {
			target = document.querySelector('.pgc-declare-original-checkbox') ||
			         document.querySelector('input[name="original"]') ||
			         document.querySelector('input[value="original"]');
		}
		if (target) {
			let input = target.tagName === 'INPUT' ? target : target.querySelector('input[type="checkbox"]');
			if (input) {
				if (!input.checked) {
					input.click();
					input.dispatchEvent(new Event('change', { bubbles: true }));
				}
				return input.checked;
			}
		}
		return false;
	}`)

	if errCheck == nil && resChecked != nil && resChecked.Value.Bool() {
		if physicalClicked {
			log.Info("Successfully set '原创/首发' label via physical click")
		} else {
			log.Info("Successfully set '原创/首发' label via JS click fallback")
		}
	} else {
		log.Warnf("Failed to verify/check '原创/首发' checkbox: %v", errCheck)
	}

	// 清理临时标记
	_, _ = a.page.Eval(`() => {
		document.querySelectorAll('.mcp-target-to-click').forEach(el => el.classList.remove('mcp-target-to-click'));
	}`)
}

// setFictionDeclaration 勾选“取材网络，虚构演绎”作品声明
func (a *ArticlePublishAction) setFictionDeclaration() {
	log.Info("Attempting to set '虚构演绎' (取材网络，虚构演绎) label...")

	// 确保展开“发文设置”
	_, _ = a.page.Timeout(3 * time.Second).Eval(`() => {
		// 寻找任何可能代表虚构声明的选项。如果存在，说明抽屉原本就是展开的，直接退出，防止重复点击将其反向折叠！
		let fictionFound = Array.from(document.querySelectorAll('span, label, div, p')).find(el => {
			let text = el.textContent ? el.textContent.trim() : '';
			return (text.includes('取材网络') || text.includes('虚构演绎') || text.includes('故事经历')) && el.offsetWidth > 0;
		});
		if (!fictionFound) {
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

	// 清理已有的临时类名并隐藏障碍物
	a.dismissObstacles()
	_, _ = a.page.Eval(`() => {
		document.querySelectorAll('.mcp-target-to-click').forEach(el => el.classList.remove('mcp-target-to-click'));
	}`)

	// 1. JS 定位虚构Checkbox包装元素，滚动并打上临时标记
	res, err := a.page.Timeout(5 * time.Second).Eval(`() => {` + SafeScrollJS + `
		let target = Array.from(document.querySelectorAll('span, label, p')).find(el => {
			let text = el.textContent ? el.textContent.trim() : '';
			return (text.includes('虚构演绎') || (text.includes('取材网络') && text.includes('虚构演绎')) || text.includes('故事经历'));
		});
		if (target) {
			let clickTarget = target.closest('label') || target;
			if (clickTarget) {
				scrollIntoViewSafe(clickTarget);
				clickTarget.classList.add('mcp-target-to-click');
				return true;
			}
		}
		return false;
	}`)

	if err != nil || res == nil || !res.Value.Bool() {
		log.Warnf("JS定位虚构Checkbox失败: %v", err)
		return
	}

	time.Sleep(500 * time.Millisecond)

	// 2. Go 物理点击标记元素
	clickEl, err := a.page.Timeout(3 * time.Second).Element(".mcp-target-to-click")
	physicalClicked := false
	if err == nil && clickEl != nil {
		pt, errPt := clickEl.Interactable()
		if errPt == nil {
			log.Infof("Physically clicking '虚构演绎' checkbox wrapper at (%f, %f)", pt.X, pt.Y)
			_ = a.page.Mouse.MoveTo(*pt)
			_ = a.page.Mouse.Down(proto.InputMouseButtonLeft, 1)
			_ = a.page.Mouse.Up(proto.InputMouseButtonLeft, 1)
			physicalClicked = true
		} else {
			log.Warnf("Failed to get interactable point for fiction checkbox wrapper: %v", errPt)
		}
	}

	time.Sleep(1 * time.Second)

	// 3. 检查状态，若未成功执行 JS 勾选兜底
	resChecked, errCheck := a.page.Eval(`() => {
		let target = document.querySelector('.mcp-target-to-click');
		if (!target) {
			target = Array.from(document.querySelectorAll('span, label, div, p')).find(el => {
				let text = el.textContent ? el.textContent.trim() : '';
				return (text.includes('虚构演绎') || (text.includes('取材网络') && text.includes('虚构演绎')) || text.includes('故事经历'));
			});
		}
		if (target) {
			let label = target.closest('label') || target;
			let input = label.querySelector('input[type="checkbox"]') || label;
			if (input) {
				if (!input.checked) {
					input.click();
					input.dispatchEvent(new Event('change', { bubbles: true }));
				}
				return input.checked;
			}
		}
		return false;
	}`)

	if errCheck == nil && resChecked != nil && resChecked.Value.Bool() {
		if physicalClicked {
			log.Info("Successfully checked '虚构演绎' checkbox via physical click")
		} else {
			log.Info("Successfully checked '虚构演绎' checkbox via JS click fallback")
		}
	} else {
		log.Warnf("Failed to verify/check '虚构演绎' checkbox: %v", errCheck)
	}

	// 清理临时标记
	_, _ = a.page.Eval(`() => {
		document.querySelectorAll('.mcp-target-to-click').forEach(el => el.classList.remove('mcp-target-to-click'));
	}`)
}

func (a *ArticlePublishAction) clickPublish(opts *ArticleOptions) error {
	// 在最终点击发布按钮之前，再次执行 Mock 注入，确保在发布校验阶段全局数据和 API 请求安全
	_, _ = a.page.Eval(StarOrderMockJS)

	if opts != nil && opts.PublishTime != nil {
		clickedSchedule, scheduleInfo, errSchedule := clickVisiblePageButtonByText(
			a.page,
			[]string{"定时发布", "预览并定时发布"},
			[]string{"预览并发布", "确认发布", "确认发表", "返回编辑", "取消"},
			"initial scheduled publish button",
		)
		if errSchedule == nil && scheduleInfo != "" {
			log.Infof("定时发布首选按钮点击结果: %s", scheduleInfo)
		}
		if errSchedule == nil && clickedSchedule {
			time.Sleep(1500 * time.Millisecond)
			screenshotPath := "./publish_after_first_click.png"
			_ = a.page.MustScreenshot(screenshotPath)
			log.Infof("Saved first-click screenshot to: %s", screenshotPath)
			return a.clickScheduledPublishConfirm(opts.PublishTime)
		}
		log.Warnf("未能直接点击编辑页定时发布按钮，将回退到预览并发布流程: %v", errSchedule)
	}

	// 1. 先查找我们将要点击的发布按钮
	el, sel, err := findElement(a.page, 3*time.Second, ArticlePublishButtonSelectors)
	if err != nil {
		return fmt.Errorf("publish button not found: %w", err)
	}
	log.Infof("Found publish button using selector: %s", sel)

	// 获取该按钮的文本
	btnTextVal, _ := el.Eval(`() => this.textContent || ''`)
	btnText := ""
	if btnTextVal != nil {
		btnText = strings.TrimSpace(btnTextVal.Value.Str())
	}
	log.Infof("Publish button text is: %s", btnText)

	// 获取 OuterHTML 和 disabled 状态用于诊断
	if htmlVal, errHtml := el.Eval(`() => this.outerHTML`); errHtml == nil && htmlVal != nil {
		log.Infof("Publish button OuterHTML: %s", htmlVal.Value.Str())
	}
	if disVal, errDis := el.Eval(`() => this.disabled`); errDis == nil && disVal != nil {
		log.Infof("Publish button disabled state: %v", disVal.Value.Bool())
	}

	// 滚动到该元素并清除浮动遮挡物
	_, _ = el.Eval(`() => {` + SafeScrollJS + `
		scrollIntoViewSafe(this);
	}`)
	time.Sleep(500 * time.Millisecond)
	a.dismissObstacles()
	time.Sleep(500 * time.Millisecond)

	// 尝试物理点击与 JS 智能兜底触发发布按钮
	var clickErr error
	pt, ptErr := el.Interactable()
	clickedPhysically := false
	if ptErr == nil {
		log.Infof("Clicking publish button via physical mouse click at point: (%f, %f)", pt.X, pt.Y)
		_ = a.page.Mouse.MoveTo(*pt)
		_ = a.page.Mouse.Down(proto.InputMouseButtonLeft, 1)
		_ = a.page.Mouse.Up(proto.InputMouseButtonLeft, 1)
		clickedPhysically = true
		time.Sleep(1500 * time.Millisecond) // 给 1.5 秒让页面发生状态跳转或弹出二次确认框
	} else {
		log.Warnf("Failed to get interactable point for publish button: %v", ptErr)
	}

	// 智能判定是否需要 JS 强力合成事件进行二次补发（避免因物理点击成功后的重复触发导致 React 组件或接口状态紊乱）
	needJSSynthesizedClick := true
	if clickedPhysically {
		info, errInfo := a.page.Info()
		if errInfo == nil && info != nil {
			if !strings.Contains(info.URL, "/graphic/publish") {
				log.Info("物理点击后页面已发生跳转，无需派发 JS 兜底点击")
				needJSSynthesizedClick = false
			}
		}
		if needJSSynthesizedClick {
			// 检测是否已经出现了二次确认弹窗
			hasVisiblePublishModal := false
			if resModal, errModal := a.page.Eval(`() => {
				const visible = (el) => {
					if (!el) return false;
					const style = window.getComputedStyle(el);
					const rect = el.getBoundingClientRect();
					return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
				};
				return Array.from(document.querySelectorAll('.semi-modal, .byte-modal, [role="dialog"], [class*="modal"], [class*="dialog"]')).some(el => {
					const text = (el.textContent || '').trim();
					return visible(el) && (text.includes('确认发布') || text.includes('确认发表') || text.includes('确定发布') || text.includes('发布后'));
				});
			}`); errModal == nil && resModal != nil {
				hasVisiblePublishModal = resModal.Value.Bool()
			}
			if hasVisiblePublishModal {
				log.Info("物理点击后检测到可见二次确认弹窗，无需派发 JS 兜底点击")
				needJSSynthesizedClick = false
			}
		}
	}

	if needJSSynthesizedClick {
		log.Info("物理点击未触发状态变更，正在派发 JS 强力合成事件链以确保发布按钮被成功触发...")
		_, clickErr = el.Timeout(5 * time.Second).Eval(`() => {
			const events = ['pointerdown', 'mousedown', 'pointerup', 'mouseup', 'click'];
			events.forEach(name => {
				const ev = new MouseEvent(name, { bubbles: true, cancelable: true, view: window });
				this.dispatchEvent(ev);
			});
		}`)
		if clickErr != nil {
			return fmt.Errorf("failed to click publish button via JS: %w", clickErr)
		}
	}

	// 保存点击后的截图用于调试分析
	screenshotPath := "./publish_after_first_click.png"
	time.Sleep(1 * time.Second) // 稍微等一秒再截图，防止截到空白
	_ = a.page.MustScreenshot(screenshotPath)
	log.Infof("Saved first-click screenshot to: %s", screenshotPath)

	if opts != nil && opts.PublishTime != nil {
		return a.clickScheduledPublishConfirm(opts.PublishTime)
	}

	// 关键判定：
	// 若点击的按钮本身即为“发布”字样（如修改文章时的直发底栏），
	// 修改文章场景下头条通常会弹出“确认修改发布”二次确认弹窗，需要短暂等待并检测。
	if btnText == "发布" {
		log.Info("点击的按钮文本是'发布'，短暂等待检测是否有二次确认弹窗...")
		// 给页面 5 秒时间弹出二次确认 Modal，共循环 10 次
		for i := 0; i < 10; i++ {
			time.Sleep(500 * time.Millisecond)

			// 检测是否已跳转（说明不需要二次确认，直接成功了）
			infoChk, errChk := a.page.Info()
			if errChk == nil && infoChk != nil && !strings.Contains(infoChk.URL, "/graphic/publish") {
				log.Infof("点击'发布'后页面已跳转（URL: %s），判定发布成功", infoChk.URL)
				return nil
			}

			// 在特定间隔清理遮罩并重试触发点击以保证动作被执行
			if i == 2 || i == 5 || i == 8 {
				log.Infof("等待中 (轮次 %d)... 清除页面遮罩并再次尝试触发'发布'按钮...", i)
				a.dismissObstacles()
				// 重试触发按钮点击
				if elRetry, _, errRetry := findElement(a.page, 1*time.Second, ArticlePublishButtonSelectors); errRetry == nil && elRetry != nil {
					_, _ = elRetry.Eval(`() => {
						const events = ['pointerdown', 'mousedown', 'pointerup', 'mouseup', 'click'];
						events.forEach(name => {
							const ev = new MouseEvent(name, { bubbles: true, cancelable: true, view: window });
							this.dispatchEvent(ev);
						});
					}`)
				}
			}

			// 检测二次确认弹窗
			confirmRes, errEval := a.page.Eval(`() => {
				let modal = document.querySelector('.semi-modal, .byte-modal, [class*="modal"], [class*="dialog"]');
				if (modal && modal.offsetWidth > 0) {
					let buttons = Array.from(modal.querySelectorAll('button')).filter(b => b.offsetWidth > 0 && b.offsetHeight > 0);
					let confirmBtn = buttons.find(b => {
						let t = b.textContent ? b.textContent.trim() : '';
						return t === '确认发布' || t === '确认发表' || t === '确定' || t === '确认';
					});
					if (!confirmBtn && buttons.length > 0) {
						confirmBtn = buttons[buttons.length - 1];
					}
					if (confirmBtn) {
						confirmBtn.click();
						return JSON.stringify({ clicked: true, text: confirmBtn.textContent.trim() });
					}
				}
				return JSON.stringify({ clicked: false });
			}`)
			if errEval == nil && confirmRes != nil {
				jsStr := confirmRes.Value.Str()
				if strings.Contains(jsStr, `"clicked":true`) {
					log.Infof("检测到修改文章的二次确认弹窗并已点击确认: %s", jsStr)
					break
				}
			}
			if clicked, info, errClick := clickVisibleModalPrimaryButton(a.page, []string{"确认发布", "确认发表", "确定", "确认", "发布"}, "update publish confirm modal"); errClick == nil && clicked {
				log.Infof("通过通用 Modal 主按钮处理修改发布确认: %s", info)
				break
			}
			if clicked, info, errClick := clickVisiblePageButtonByText(a.page, []string{"确认发布", "确认发表", "确定发布", "发布"}, []string{"预览并发布", "预览并定时发布", "返回编辑", "取消"}, "update publish confirm page"); errClick == nil && clicked {
				log.Infof("通过页面确认按钮处理修改发布确认: %s", info)
				break
			}
		}
		return waitForPublishResult(a.page, 90*time.Second)
	}

	// 轮询等待二次确认弹窗中的“确认发布”按钮并进行点击，最多等待 10 秒
	var clickedConfirm bool
	var lastJSResult string
	for i := 0; i < 20; i++ {
		// 优先检测是否已经成功跳转，如果已经跳转，直接代表发布完成！
		info, errInfo := a.page.Info()
		if errInfo == nil && info != nil {
			if !strings.Contains(info.URL, "/graphic/publish") {
				log.Infof("检测到页面已完成跳转（当前 URL: %s），直接判定发布成功，跳过二次确认", info.URL)
				clickedConfirm = true
				break
			}
		}

		clicked, modalInfo, err := clickVisibleModalPrimaryButton(a.page, []string{"确认发布", "确认发表", "确定发布", "确定", "确认", "发布"}, "publish confirm modal")
		if err == nil && modalInfo != "" {
			lastJSResult = modalInfo
		}
		if err == nil && clicked {
			log.Infof("Secondary publish confirmation succeeded: %s", modalInfo)
			clickedConfirm = true
			break
		}
		clickedPage, pageInfo, errPage := clickVisiblePageButtonByText(a.page, []string{"确认发布", "确认发表", "确定发布", "发布"}, []string{"预览并发布", "预览并定时发布", "返回编辑", "取消"}, "publish confirm page")
		if errPage == nil && pageInfo != "" {
			lastJSResult = pageInfo
		}
		if errPage == nil && clickedPage {
			log.Infof("Secondary publish page confirmation succeeded: %s", pageInfo)
			clickedConfirm = true
			break
		}

		// 截图显示可能停留在底部“预览并发布”按钮，没有真实确认弹窗；此时应重试底部主按钮，而不是误判弹窗已出现。
		if i == 3 || i == 8 || i == 14 {
			resRetry, errRetry := a.page.Eval(`() => {
				const visible = (el) => {
					if (!el) return false;
					const style = window.getComputedStyle(el);
					const rect = el.getBoundingClientRect();
					return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
				};
				const buttons = Array.from(document.querySelectorAll('button')).filter(visible);
				const btn = buttons.find(b => {
					const text = (b.textContent || '').trim();
					return text === '预览并发布' || text === '发布' || text === '预览并定时发布';
				});
				if (!btn) return JSON.stringify({ clicked: false });
				btn.click();
				return JSON.stringify({ clicked: true, text: (btn.textContent || '').trim() });
			}`)
			if errRetry == nil && resRetry != nil {
				log.Infof("重试底部发布主按钮结果: %s", resRetry.Value.Str())
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	if !clickedConfirm {
		log.Warnf("Secondary publish confirmation button not found or click failed. Last JS result: %s", lastJSResult)
	}

	time.Sleep(1 * time.Second)

	// 等待并检测发布结果，最多等待 90 秒（包含跳转时间）
	return waitForPublishResult(a.page, 90*time.Second)
}

func (a *ArticlePublishAction) clickScheduledPublishConfirm(publishTime interface{}) error {
	log.Info("检测到定时发布参数，进入预览确认页后的定时发布流程...")
	var lastResult string

	for i := 0; i < 20; i++ {
		info, errInfo := a.page.Info()
		if errInfo == nil && info != nil && !strings.Contains(info.URL, "/graphic/publish") {
			log.Infof("定时发布确认前页面已跳转（当前 URL: %s），判定流程已提交", info.URL)
			return nil
		}

		if modalEl, errModal := findExistingTimingModal(a.page, "scheduled-confirm-loop"); errModal == nil && modalEl != nil {
			if err := setPublishTime(a.page, publishTime); err != nil {
				lastResult = err.Error()
				log.Errorf("预览页定时弹窗设置失败，中断本次提交以避免默认时间误发: %v", err)
				return fmt.Errorf("设置定时发布时间失败: %w", err)
			} else {
				if err := waitForPublishResult(a.page, 25*time.Second); err != nil {
					log.Warnf("定时发布提交后未检测到跳转或成功提示，继续交由发布后列表校验确认: %v", err)
				}
				return nil
			}
		}

		onPreviewPage := false
		previewRes, previewErr := a.page.Eval(`() => {
			const text = document.body ? (document.body.innerText || '').replace(/\s+/g, '') : '';
			return text.includes('返回编辑') &&
				(text.includes('确认发布') || text.includes('发布文章') || text.includes('定时发布') || text.includes('预览并定时发布'));
		}`)
		if previewErr == nil && previewRes != nil {
			onPreviewPage = previewRes.Value.Bool()
		}
		if !onPreviewPage {
			if i == 6 {
				clickedPreview, previewInfo, errPreview := clickVisiblePageButtonByText(
					a.page,
					[]string{"预览并发布", "发布"},
					[]string{"定时发布", "预览并定时发布", "确认发布", "返回编辑", "取消"},
					"scheduled fallback preview publish button",
				)
				if errPreview == nil && previewInfo != "" {
					lastResult = previewInfo
					log.Warnf("定时发布按钮未触发弹窗或确认页，已改点预览并发布兜底: %s", previewInfo)
				}
				if errPreview == nil && clickedPreview {
					time.Sleep(1500 * time.Millisecond)
					continue
				}
				log.Warnf("点击发布主按钮后仍未进入预览确认页，跳过定时设置并等待页面发布结果。最后状态: %s", lastResult)
				return waitForPublishResult(a.page, 45*time.Second)
			}
			time.Sleep(500 * time.Millisecond)
			continue
		}

		clicked, infoClick, errClick := clickVisiblePageButtonByText(
			a.page,
			[]string{"预览并定时发布", "定时发布"},
			[]string{"预览并发布", "确认发布", "确认发表", "立即发布", "返回编辑", "取消"},
			"scheduled publish confirm page",
		)
		if errClick == nil && infoClick != "" {
			lastResult = infoClick
		}
		if errClick == nil && clicked {
			log.Infof("已点击预览确认页定时发布按钮: %s", infoClick)
			time.Sleep(1500 * time.Millisecond)
			if err := setPublishTime(a.page, publishTime); err != nil {
				lastResult = err.Error()
				log.Errorf("点击定时发布按钮后设置时间失败，中断本次提交以避免默认时间误发: %v", err)
				return fmt.Errorf("设置定时发布时间失败: %w", err)
			} else {
				if err := waitForPublishResult(a.page, 25*time.Second); err != nil {
					log.Warnf("定时发布提交后未检测到跳转或成功提示，继续交由发布后列表校验确认: %v", err)
				}
				return nil
			}
		}

		clickedConfirm, confirmInfo, errConfirm := clickVisiblePageButtonByText(
			a.page,
			[]string{"确认发布", "确定发布", "发布文章", "发布"},
			[]string{"预览并发布", "预览并定时发布", "返回编辑", "取消"},
			"scheduled publish fallback confirm page",
		)
		if errConfirm == nil && confirmInfo != "" {
			lastResult = confirmInfo
		}
		if errConfirm == nil && clickedConfirm {
			log.Warnf("预览确认页未找到定时发布按钮，已降级点击确认发布: %s", confirmInfo)
			return waitForPublishResult(a.page, 90*time.Second)
		}

		if i == 5 || i == 12 {
			resRetry, errRetry := a.page.Eval(`() => {
				const visible = (el) => {
					if (!el) return false;
					const style = window.getComputedStyle(el);
					const rect = el.getBoundingClientRect();
					return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
				};
				const btn = Array.from(document.querySelectorAll('button, [role="button"], .byte-btn, .semi-button')).find(el => {
					const text = (el.textContent || '').replace(/\s+/g, '').trim();
					return visible(el) && (text === '预览并发布' || text === '预览并定时发布');
				});
				if (!btn) return JSON.stringify({ clicked: false });
				btn.click();
				return JSON.stringify({ clicked: true, text: (btn.textContent || '').trim() });
			}`)
			if errRetry == nil && resRetry != nil {
				log.Infof("定时发布流程重试底部主按钮结果: %s", resRetry.Value.Str())
				lastResult = resRetry.Value.Str()
			}
		}

		time.Sleep(500 * time.Millisecond)
	}

	log.Warnf("定时发布确认按钮未能完成点击或识别，最后结果: %s", lastResult)
	return waitForPublishResult(a.page, 90*time.Second)
}

// truncateStr 截取字符串，超出长度加省略号
func truncateStr(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

// Update 修改/更新已有文章
func (a *ArticlePublishAction) Update(ctx context.Context, articleID string, title, content string, opts *ArticleOptions) error {
	if strings.TrimSpace(articleID) == "" {
		return fmt.Errorf("articleID is required for update")
	}

	// 启用网络请求劫持，拦截并过滤星图 null 响应，防御前端 JS 崩溃
	router := a.page.HijackRequests()
	_ = router.Add("*", "*", func(ctx *rod.Hijack) {
		reqURL := ctx.Request.URL().String()
		if shouldBypassHijack(reqURL, ctx.Request.Method(), ctx.Request.Header("Content-Type")) {
			ctx.ContinueRequest(&proto.FetchContinueRequest{})
			return
		}
		err := ctx.LoadResponse(http.DefaultClient, true)
		if err != nil {
			return
		}
		body := ctx.Response.Body()

		if strings.Contains(body, "star") {
			body = starNullRe.ReplaceAllStringFunc(body, func(match string) string {
				submatches := starNullRe.FindStringSubmatch(match)
				if len(submatches) >= 2 {
					key := submatches[1]
					lowerKey := strings.ToLower(key)
					if strings.Contains(lowerKey, "orderinfo") || strings.Contains(lowerKey, "order_info") {
						return fmt.Sprintf(`"%s":{}`, key)
					}
					return fmt.Sprintf(`"%s":{"starOrderInfo":{},"star_order_info":{}}`, key)
				}
				return match
			})
			ctx.Response.SetBody(body)
		}
	})
	go router.Run()
	defer router.Stop()

	// 1. 拼接修改已有文章的 URL 并导航
	editURL := fmt.Sprintf("https://mp.toutiao.com/profile_v4/graphic/publish?pgc_id=%s", articleID)
	log.Infof("正在导航到文章编辑页面: %s", editURL)

	// 注册新页面自动注入脚本以防御 starOrderInfo null 导致的 JS 崩溃
	if _, err := a.page.EvalOnNewDocument(StarOrderMockJS); err != nil {
		log.Warnf("【Mock 注入】注册 EvalOnNewDocument 失败: %v", err)
	}

	if err := a.page.Navigate(editURL); err != nil {
		return fmt.Errorf("failed to navigate to edit page: %w", err)
	}
	if err := a.page.WaitLoad(); err != nil {
		return fmt.Errorf("failed to wait for edit page load: %w", err)
	}
	time.Sleep(3 * time.Second)

	// 注入请求监听
	_, _ = a.page.Eval(NetworkTrackerJS)

	// 2. 确保已登录
	if err := EnsureLogin(a.page, a.cookieStore); err != nil {
		return err
	}

	// 再次导航到编辑页以防刚才扫码弹窗影响
	if err := a.page.Navigate(editURL); err != nil {
		return fmt.Errorf("failed to navigate to edit page: %w", err)
	}
	_ = a.page.Timeout(15 * time.Second).WaitLoad()

	// 强行清空本地 LocalStorage 和 SessionStorage 缓存，防止之前的残留草稿被头条编辑器恢复出来
	_, _ = a.page.Eval(`() => {
		try {
			window.localStorage.clear();
			window.sessionStorage.clear();
		} catch(e) {}
	}`)
	time.Sleep(2 * time.Second)

	// 轮询等待编辑器异步数据填充完毕（最大等待 10 秒），防止写入太快被后续拉取到的原有内容覆写冲掉
	log.Info("等待编辑器异步加载并填充原有文章内容...")
	for i := 0; i < 20; i++ {
		titleEl, _, errT := findElement(a.page, 500*time.Millisecond, ArticleTitleSelectors)
		contentEl, _, errC := findElement(a.page, 500*time.Millisecond, ArticleContentSelectors)
		if errT == nil && errC == nil && titleEl != nil && contentEl != nil {
			titleVal, _ := titleEl.Eval(`() => this.value || ''`)
			contentVal, _ := contentEl.Eval(`() => this.innerText || this.textContent || ''`)
			if titleVal != nil && contentVal != nil {
				tText := strings.TrimSpace(titleVal.Value.Str())
				cText := strings.TrimSpace(contentVal.Value.Str())
				if tText != "" || cText != "" {
					log.Infof("检测到编辑器数据已填充完毕（标题长度: %d, 正文长度: %d）", len(tText), len(cText))
					break
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	time.Sleep(3 * time.Second) // 平息期：等待 3 秒让 React 异步回调渲染全部完成，防止后续写入被重绘冲掉

	// 在编辑器数据填充完毕后，再次手动触发一次 Mock 注入，双重保险
	_, _ = a.page.Eval(StarOrderMockJS)

	// 3. 修改标题
	if title != "" {
		log.Infof("正在修改标题为: %s", title)
		if err := a.inputTitle(title); err != nil {
			return fmt.Errorf("failed to input updated title: %w", err)
		}
		time.Sleep(1 * time.Second)
	}

	// 4. 修改正文
	if content != "" {
		log.Info("正在修改正文内容...")
		if err := a.inputContent(content, opts); err != nil {
			return fmt.Errorf("failed to input updated content: %w", err)
		}
		time.Sleep(1 * time.Second)

		if err := a.verifyContent(); err != nil {
			log.Warnf("正文内容更新验证警告: %v", err)
		}

		// 解析新正文里的所有图片并进行必要的自适应封面图分配
		blocks := parseContentBlocks(content)
		var inlineImages []string
		for _, b := range blocks {
			if b.Type == "image" && b.Value != "" {
				inlineImages = append(inlineImages, b.Value)
			}
		}

		// 只有在显式重新指定了封面参数，或者自适应模式需要的情况下，我们才更新封面
		var targetCoverMode string
		var targetCovers []string
		var isAutoCover bool

		if opts != nil && opts.CoverImage != "" {
			targetCoverMode = "单图"
			targetCovers = []string{opts.CoverImage}
		} else if opts != nil && len(opts.Images) > 0 && len(inlineImages) == 0 {
			if len(opts.Images) >= 3 {
				targetCoverMode = "三图"
				targetCovers = opts.Images[:3]
			} else {
				targetCoverMode = "单图"
				targetCovers = []string{opts.Images[0]}
			}
		} else {
			// 如果没指定任何 opts（或者指定的 opts 中没有封面），且正文变了，我们重新自适应
			if len(inlineImages) >= 3 {
				targetCoverMode = "三图"
				targetCovers = inlineImages[:3]
				isAutoCover = true
			} else if len(inlineImages) > 0 {
				targetCoverMode = "单图"
				targetCovers = []string{inlineImages[0]}
				isAutoCover = true
			} else {
				targetCoverMode = "无封面"
			}
		}

		if targetCoverMode != "" {
			log.Infof("重新决策更新封面: 模式=%s, 图片数=%d, 自适应=%t", targetCoverMode, len(targetCovers), isAutoCover)

			// 在修改文章时，若页面上对应的封面插槽已经有上传完成的图片了，则跳过重传以提升速度并防止超时
			needUpload := false
			if targetCoverMode == "无封面" {
				needUpload = false
			} else {
				for i := 0; i < len(targetCovers); i++ {
					hasImg, errCheck := a.checkCoverSlotHasImage(i)
					if errCheck != nil || !hasImg {
						needUpload = true
						break
					}
				}
			}

			if err := a.setCoverMode(targetCoverMode); err != nil {
				log.Warnf("Failed to set cover mode to %s: %v", targetCoverMode, err)
			} else if len(targetCovers) > 0 && needUpload {
				if isAutoCover {
					time.Sleep(3 * time.Second)
				}
				if err := a.uploadCovers(targetCovers, isAutoCover); err != nil {
					log.Warnf("Failed to upload cover images: %v", err)
				}
			} else if !needUpload && targetCoverMode != "无封面" {
				log.Infof("检测到修改页面封面插槽已有上传完成的封面图，跳过封面重传流程")
			}
		}
	}

	// 5. 修改选项 (原创/虚构)
	if opts != nil {
		if opts.Original {
			a.setOriginal()
		}
		if opts.Fiction {
			a.setFictionDeclaration()
		}
	}

	time.Sleep(1 * time.Second)

	// 诊断 React 状态与警告提示
	if diagRes, diagErr := a.page.Eval(`() => {
		let results = [];
		// 1. 查找所有可能显示错误或警告提示的可见元素
		let alertEls = Array.from(document.querySelectorAll('*')).filter(el => {
			if (el.offsetWidth === 0 || el.offsetHeight === 0) return false;
			let className = el.className && typeof el.className === 'string' ? el.className.toLowerCase() : '';
			let id = el.id && typeof el.id === 'string' ? el.id.toLowerCase() : '';
			return className.includes('error') || className.includes('warning') || className.includes('alert') || 
			       className.includes('invalid') || className.includes('tip') || className.includes('danger') ||
			       id.includes('error') || id.includes('warning') || id.includes('alert') || id.includes('invalid');
		});
		alertEls.forEach(el => {
			if (el.textContent && el.textContent.trim().length > 0) {
				results.push('Alert Element (' + el.tagName + ', class=' + el.className + '): ' + el.textContent.trim());
			}
		});

		// 2. 检查标题 input 的 React 状态
		let titleEl = document.querySelector("textarea[placeholder*='请输入文章标题']");
		if (titleEl) {
			results.push('Title DOM Value: "' + titleEl.value + '"');
			let reactKeys = Object.keys(titleEl).filter(k => k.startsWith('__reactFiber$') || k.startsWith('__reactInternalInstance$'));
			if (reactKeys.length > 0) {
				let fiber = titleEl[reactKeys[0]];
				if (fiber && fiber.memoizedProps) {
					results.push('Title React memoizedProps value: "' + fiber.memoizedProps.value + '"');
				}
				// 遍历 fiber 树寻找 React State 里的值
				let curr = fiber;
				while (curr) {
					if (curr.memoizedState && curr.memoizedState.memoizedState !== undefined) {
						results.push('React State Value found: ' + curr.memoizedState.memoizedState);
					}
					curr = curr.return;
				}
			}
		}

		// 3. 检查编辑器（ProseMirror）的文本字数
		let editorEl = document.querySelector(".ProseMirror");
		if (editorEl) {
			results.push('Editor DOM Text Length: ' + (editorEl.textContent || '').length);
		}
		return results;
	}`); diagErr == nil && diagRes != nil {
		log.Infof("【DOM 状态诊断】:")
		for _, item := range diagRes.Value.Arr() {
			log.Infof(" - %s", item.Str())
		}
	}

	// 最终发布前一致性二次固化校验（AGENTS.md 规则 #8）：
	// 检查标题是否被 React 重绘冲掉，如果是，则通过 React Value Tracker 劫持技术重新强行注入
	if title != "" {
		titleCheckEl, _, errCheck := findElement(a.page, 2*time.Second, ArticleTitleSelectors)
		if errCheck == nil && titleCheckEl != nil {
			currentVal, evalErr := titleCheckEl.Eval(`() => this.value || ''`)
			if evalErr == nil && currentVal != nil {
				currentTitle := strings.TrimSpace(currentVal.Value.Str())
				if currentTitle != title {
					log.Warnf("【二次固化】检测到标题被 React 重绘冲掉！当前值: '%s', 目标值: '%s'，正在重新注入...", truncateStr(currentTitle, 20), truncateStr(title, 20))
					// 使用 React Value Tracker 劫持技术强行注入
					_, _ = titleCheckEl.Eval(`val => {
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
							setter.call(this, val);
						} else {
							this.value = val;
						}
						const tracker = this._valueTracker;
						if (tracker) {
							tracker.setValue(val);
						}
						this.dispatchEvent(new Event('input', {bubbles: true}));
						this.dispatchEvent(new Event('change', {bubbles: true}));
						
						// 遍历 React Fiber 绑定的事件处理器，主动触发 onChange/onInput
						let reactKeys = Object.keys(this).filter(k => k.startsWith('__reactProps$') || k.startsWith('__reactEventHandlers$'));
						for (let key of reactKeys) {
							let ho = this[key];
							if (ho && ho.onChange) ho.onChange({ target: this, currentTarget: this });
							if (ho && ho.onInput) ho.onInput({ target: this, currentTarget: this });
						}
					}`, title)
					time.Sleep(500 * time.Millisecond)
					// 验证二次注入结果
					if recheck, recheckErr := titleCheckEl.Eval(`() => this.value || ''`); recheckErr == nil && recheck != nil {
						log.Infof("【二次固化】标题重新注入后的值: '%s'", truncateStr(strings.TrimSpace(recheck.Value.Str()), 30))
					}
				} else {
					log.Infof("【二次固化】标题一致性校验通过，当前值与目标值一致: '%s'", truncateStr(currentTitle, 30))
				}
			}
		}
	}

	// 6. 重新点击发布或保存为草稿
	if opts != nil && opts.SaveAsDraft {
		log.Info("检测到 SaveAsDraft 为 true，等待自动保存草稿并退出...")
		time.Sleep(8 * time.Second)
	} else {
		if err := a.clickPublish(opts); err != nil {
			return fmt.Errorf("failed to publish updated article: %w", err)
		}
	}

	log.Infof("Article %s updated and published successfully", articleID)
	return nil
}

// StarOrderMockJS 注入代码，深度修复 API 响应或全局变量中 starOrderInfo 相关的 null 数据，防止头条号前端 JS 崩溃
const StarOrderMockJS = `(() => {
	function deepFixNull(obj, visited = new WeakSet()) {
		if (obj === null || obj === undefined) return obj;
		if (typeof obj !== 'object') return obj;
		
		// 排除 DOM 元素和 window 等宿主对象，防止循环引用或安全限制报错
		if (obj.nodeType || obj === window || obj === document || (obj.constructor && obj.constructor.name === 'Window')) {
			return obj;
		}

		// 检查循环引用
		if (visited.has(obj)) {
			return obj;
		}
		visited.add(obj);

		if (Array.isArray(obj)) {
			for (let i = 0; i < obj.length; i++) {
				obj[i] = deepFixNull(obj[i], visited);
			}
			return obj;
		}
		for (let key in obj) {
			if (Object.prototype.hasOwnProperty.call(obj, key)) {
				let val = obj[key];
				let lowerKey = key.toLowerCase();
				if (lowerKey.includes('star') && val === null) {
					if (lowerKey.includes('id') || lowerKey.includes('name')) {
						obj[key] = '';
					} else {
						obj[key] = { starOrderInfo: {}, star_order_info: {} };
					}
				} else if ((lowerKey.includes('starorder') || lowerKey.includes('star_order')) && val === null) {
					obj[key] = {};
				} else {
					if (val && typeof val === 'object') {
						obj[key] = deepFixNull(val, visited);
					}
				}
			}
		}
		return obj;
	}

	// 1. 拦截 window 全局变量定义，以防 inline 脚本初始化时带入 null，并在读取时实时修复子属性
	function interceptGlobal(objName) {
		let cachedVal = undefined;
		try {
			if (window[objName] !== undefined) {
				cachedVal = window[objName];
			}
			Object.defineProperty(window, objName, {
				get: function() {
					return deepFixNull(cachedVal);
				},
				set: function(val) {
					cachedVal = val;
				},
				configurable: true
			});
		} catch(e) {}
	}
	['pgc_init_data', 'pgcInitData', '__INITIAL_STATE__'].forEach(interceptGlobal);

	// 2. 劫持 Response 原型链方法，在前端调用反序列化时自动修复数据，不影响 Response 的生命周期和属性
	try {
		if (window.Response && window.Response.prototype && !window.Response.prototype.json.isMocked) {
			const originalJson = window.Response.prototype.json;
			window.Response.prototype.json = async function() {
				const data = await originalJson.apply(this);
				return deepFixNull(data);
			};
			window.Response.prototype.json.isMocked = true;
		}
		if (window.Response && window.Response.prototype && !window.Response.prototype.text.isMocked) {
			const originalText = window.Response.prototype.text;
			window.Response.prototype.text = async function() {
				const text = await originalText.apply(this);
				try {
					const data = JSON.parse(text);
					const fixed = deepFixNull(data);
					return JSON.stringify(fixed);
				} catch(e) {
					return text;
				}
			};
			window.Response.prototype.text.isMocked = true;
		}
	} catch(e) {}

	// 3. 劫持 XMLHttpRequest 原型链属性的 getter，高容错且不影响 XHR 事件周期
	try {
		if (window.XMLHttpRequest && window.XMLHttpRequest.prototype && !window.XMLHttpRequest.prototype._isMocked) {
			const proto = window.XMLHttpRequest.prototype;
			
			const descriptorText = Object.getOwnPropertyDescriptor(proto, 'responseText');
			if (descriptorText && descriptorText.get) {
				Object.defineProperty(proto, 'responseText', {
					get: function() {
						const val = descriptorText.get.apply(this);
						try {
							const data = JSON.parse(val);
							const fixed = deepFixNull(data);
							return JSON.stringify(fixed);
						} catch(e) {
							return val;
						}
					},
					configurable: true
				});
			}

			const descriptorRep = Object.getOwnPropertyDescriptor(proto, 'response');
			if (descriptorRep && descriptorRep.get) {
				Object.defineProperty(proto, 'response', {
					get: function() {
						const val = descriptorRep.get.apply(this);
						if (this.responseType === 'json') {
							return deepFixNull(val);
						}
						if (typeof val === 'string') {
							try {
								const data = JSON.parse(val);
								const fixed = deepFixNull(data);
								return JSON.stringify(fixed);
							} catch(e) {}
						}
						return val;
					},
					configurable: true
				});
			}
			proto._isMocked = true;
		}
	} catch(e) {}
})();`
