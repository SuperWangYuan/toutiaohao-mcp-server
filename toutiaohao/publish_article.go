package toutiaohao

import (
	"context"
	"fmt"
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

// ArticleOptions 文章发布可选参数
type ArticleOptions struct {
	Images      []string    `json:"images,omitempty"`
	Tags        []string    `json:"tags,omitempty"`
	Category    string      `json:"category,omitempty"`
	CoverImage  string      `json:"cover_image,omitempty"`
	Original    bool        `json:"original,omitempty"`
	Fiction     bool        `json:"fiction,omitempty"`
	PublishTime interface{} `json:"publish_time,omitempty"`
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
	if err := a.inputContent(content); err != nil {
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

	if opts != nil && (opts.CoverImage != "" || len(opts.Images) > 0) {
		// 1. 如果显式指定了封面参数，以 opts 参数为准
		if len(opts.Images) >= 3 {
			targetCoverMode = "三图"
			targetCovers = opts.Images[:3]
		} else {
			targetCoverMode = "单图"
			coverImg := opts.CoverImage
			if coverImg == "" && len(opts.Images) > 0 {
				coverImg = opts.Images[0]
			}
			targetCovers = []string{coverImg}
		}
	} else {
		// 2. 如果未显式指定封面参数，则根据正文中的插图数量智能自适应
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
		}
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

	// 点击发布
	if err := a.clickPublish(opts); err != nil {
		return fmt.Errorf("failed to publish: %w", err)
	}

	log.Info("Article published successfully")
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

func (a *ArticlePublishAction) inputContent(content string) error {
	el, sel, err := findElement(a.page, 3*time.Second, ArticleContentSelectors)
	if err != nil {
		return fmt.Errorf("content editor not found: %w", err)
	}
	log.Infof("Found content editor using selector: %s", sel)

	// 解析内容块
	blocks := parseContentBlocks(content)

	log.Infof("准备插入正文，共包含 %d 个文本/插图内容块...", len(blocks))

	// 先清空编辑器并 focus
	_, err = el.Eval(`() => {
		this.focus();
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
			log.Infof("正在插入图片块 %d/%d: %s", i+1, len(blocks), b.Value)
			if err := a.insertImageAtCursor(b.Value); err != nil {
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

	// 3. 上传文件
	fileInput, err := a.page.Timeout(5 * time.Second).Element(`input[type='file']`)
	if err != nil {
		return fmt.Errorf("未找到图片上传的文件输入控件(file input): %w", err)
	}
	fileInput = fileInput.CancelTimeout()

	if err := fileInput.SetFiles([]string{absPath}); err != nil {
		return fmt.Errorf("文件输入控件设置路径失败: %w", err)
	}

	// 4. 等待确认弹窗并点击确认按钮
	log.Info("等待图片上传完毕并寻找确认按钮...")
	var clickedConfirm bool
	for k := 0; k < 20; k++ { // 最多等 10 秒
		// 先清理旧标记
		_, _ = a.page.Eval(`() => {
			document.querySelectorAll('.mcp-confirm-btn').forEach(el => el.classList.remove('mcp-confirm-btn'));
		}`)

		// 检查确定按钮是否已启用，如未启用则尝试点击缩略图
		resConfirm, errEval := a.page.Eval(`() => {
			let btn = document.querySelector('[data-e2e="imageUploadConfirm-btn"]') ||
			          Array.from(document.querySelectorAll('button')).find(b => {
			              let text = b.textContent ? b.textContent.trim() : '';
			              return (text === '确定' || text === '确认') && b.offsetWidth > 0;
			          });
			if (btn) {
				let isDisabled = btn.disabled || btn.classList.contains('is-disabled') || btn.className.includes('disabled') || btn.getAttribute('disabled') !== null;
				if (!isDisabled) {
					btn.classList.add('mcp-confirm-btn');
					return { ready: true };
				}
			}

			// 确定按钮不可用，尝试点击已上传的缩略图以进行选中
			let imgs = Array.from(document.querySelectorAll('img'));
			let clickedAny = false;
			imgs.forEach(img => {
				if (img.offsetWidth > 0 && img.offsetHeight > 0) {
					if (img.closest('[class*="header"]') || img.closest('[class*="sidebar"]') || img.closest('[class*="nav"]') || img.closest('[class*="user"]')) {
						return;
					}
					// 确认属于上传列表或图片选择区域
					let wrapper = img.closest('[class*="item"]') || img.closest('[class*="card"]') || img;
					if (!wrapper.dataset.mcpClicked) {
						wrapper.click();
						wrapper.dataset.mcpClicked = "true";
						clickedAny = true;
					}
				}
			});
			return { ready: false, clicked: clickedAny };
		}`)

		if errEval == nil && resConfirm != nil {
			readyVal := resConfirm.Value.Get("ready")
			if readyVal.Val() != nil && readyVal.Bool() {
				confirmEl, errEl := a.page.Timeout(2 * time.Second).Element(".mcp-confirm-btn")
				if errEl == nil && confirmEl != nil {
					ptConfirm, errPt := confirmEl.Interactable()
					if errPt == nil {
						log.Infof("物理点击图片弹窗确认按钮，坐标 (%f, %f)", ptConfirm.X, ptConfirm.Y)
						_ = a.page.Mouse.MoveTo(*ptConfirm)
						_ = a.page.Mouse.Down(proto.InputMouseButtonLeft, 1)
						_ = a.page.Mouse.Up(proto.InputMouseButtonLeft, 1)
					} else {
						log.Warn("确认按钮不可物理点击，回退到 JS 点击")
						_, _ = confirmEl.Eval("() => this.click()")
					}
					clickedConfirm = true
				}
			}
		}

		// 清理标记
		_, _ = a.page.Eval(`() => {
			document.querySelectorAll('.mcp-confirm-btn').forEach(el => el.classList.remove('mcp-confirm-btn'));
		}`)

		if clickedConfirm {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if !clickedConfirm {
		return fmt.Errorf("未能找到或点击图片上传的确认按钮")
	}

	// 5. 等待弹窗在页面上关闭
	dialogClosed := false
	for j := 0; j < 10; j++ { // 最多等 5 秒
		resExist, errExist := a.page.Eval(`() => {
			let btn = document.querySelector('[data-e2e="imageUploadConfirm-btn"]') ||
			          Array.from(document.querySelectorAll('button')).find(b => {
			              let text = b.textContent ? b.textContent.trim() : '';
			              return (text === '确定' || text === '确认') && b.offsetWidth > 0;
			          });
			return btn ? true : false;
		}`)
		if errExist == nil && resExist != nil && !resExist.Value.Bool() {
			dialogClosed = true
			break
		}
		// 补点一次
		_, _ = a.page.Eval(`() => {
			let btn = document.querySelector('.mcp-confirm-btn');
			if (btn) btn.click();
		}`)
		time.Sleep(500 * time.Millisecond)
	}

	if dialogClosed {
		log.Info("图片上传弹窗已成功关闭，插图完成")
	} else {
		log.Warn("弹窗超时未完全关闭，可能仍然挂起，继续发文流程")
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
		// 隐藏可能覆盖按钮的遮罩层
		document.querySelectorAll('.byte-drawer-mask, .semi-drawer-mask, [class*="drawer-mask"], [class*="mask"]').forEach(el => {
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

func (a *ArticlePublishAction) uploadCovers(coverPaths []string, isAutoCover bool) error {
	log.Infof("Uploading %d cover images...", len(coverPaths))
	var cleanups []func()
	defer func() {
		for _, cleanup := range cleanups {
			cleanup()
		}
	}()

	for i, path := range coverPaths {
		// 如果是自适应封面，且该位置的封面槽已自动填入了图片，则跳过上传
		if isAutoCover {
			hasImg, errCheck := a.checkCoverSlotHasImage(i)
			if errCheck == nil && hasImg {
				log.Infof("检测到封面槽 %d 已自动填入图片，自适应模式下跳过上传", i+1)
				continue
			}
		}

		localPath, cleanup, err := downloadImageToTemp(path)
		if err != nil {
			return fmt.Errorf("准备封面图片失败 (%s): %w", path, err)
		}
		cleanups = append(cleanups, cleanup)

		log.Infof("Uploading cover %d: %s (本地路径: %s)", i+1, path, localPath)
		
		// 隐藏可能造成遮挡的页面元素
		a.dismissObstacles()

		// 先清理已有的标记类名
		_, _ = a.page.Eval(`() => {
			document.querySelectorAll('.mcp-target-to-click').forEach(el => el.classList.remove('mcp-target-to-click'));
		}`)

		// 每次上传前重新获取上传框元素，因为上传后 DOM 可能发生改变
		coverEls, err := a.page.Timeout(3 * time.Second).Elements(`div.article-cover-add, [class*='cover'][class*='add']`)
		if err != nil || len(coverEls) == 0 {
			log.Warnf("Failed to find cover add slots, trying selector...")
			coverEl, _, err := findElement(a.page, 3*time.Second, ArticleCoverAddSelectors)
			if err != nil {
				return fmt.Errorf("cover area not found for image %d: %w", i+1, err)
			}
			coverEls = rod.Elements{coverEl}
		}

		if len(coverEls) == 0 {
			return fmt.Errorf("no cover upload slots found for image %d", i+1)
		}

		targetEl := coverEls[0]
		
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

		// 模拟物理鼠标点击
		pt, err := clickEl.Interactable()
		if err == nil {
			log.Infof("Clicking cover slot %d at point (%f, %f)", i+1, pt.X, pt.Y)
			_ = a.page.Mouse.MoveTo(*pt)
			_ = a.page.Mouse.Down(proto.InputMouseButtonLeft, 1)
			_ = a.page.Mouse.Up(proto.InputMouseButtonLeft, 1)
		} else {
			log.Warnf("Failed to get interactable point for cover slot %d, fallback to JS click: %v", i+1, err)
			_, _ = clickEl.Eval("() => this.click()")
		}
		time.Sleep(1500 * time.Millisecond)

		// 清理临时标记
		_, _ = a.page.Eval(`() => {
			document.querySelectorAll('.mcp-target-to-click').forEach(el => el.classList.remove('mcp-target-to-click'));
		}`)

		// 点击“上传图片”按钮
		uploadEl, sel, err := findElement(a.page, 3*time.Second, ArticleCoverUploadSelectors)
		var clickUploadFailed bool
		if err != nil {
			log.Warnf("【封面上传】未找到封面上传按钮 (image %d): %v。将尝试直接寻找并写入 file input 控件作为兜底...", i+1, err)
			clickUploadFailed = true
		} else {
			log.Infof("Found cover upload using selector: %s", sel)

			// 对“上传图片”按钮进行安全滚动和物理点击
			_, _ = uploadEl.Eval(`() => {` + SafeScrollJS + `
				scrollIntoViewSafe(this);
				this.classList.add('mcp-target-to-click');
			}`)
			time.Sleep(500 * time.Millisecond)

			clickUploadEl, err := a.page.Timeout(3 * time.Second).Element(".mcp-target-to-click")
			if err == nil {
				ptUpload, errPt := clickUploadEl.Interactable()
				if errPt == nil {
					log.Infof("Clicking upload button at point (%f, %f)", ptUpload.X, ptUpload.Y)
					_ = a.page.Mouse.MoveTo(*ptUpload)
					_ = a.page.Mouse.Down(proto.InputMouseButtonLeft, 1)
					_ = a.page.Mouse.Up(proto.InputMouseButtonLeft, 1)
				} else {
					log.Warnf("Failed to get interactable point for upload button, fallback to direct click: %v", errPt)
					_ = clickUploadEl.Click(proto.InputMouseButtonLeft, 1)
				}
			} else {
				_ = uploadEl.Click(proto.InputMouseButtonLeft, 1)
			}
			time.Sleep(1000 * time.Millisecond)

			// 清理临时标记
			_, _ = a.page.Eval(`() => {
				document.querySelectorAll('.mcp-target-to-click').forEach(el => el.classList.remove('mcp-target-to-click'));
			}`)
		}

		// 设置文件路径
		fileInput, err := a.page.Timeout(3 * time.Second).Element(`input[type='file']`)
		if err != nil {
			if clickUploadFailed {
				safeScreenshot(a.page, "screenshot_upload_error.png")
				log.Warnf("Upload error screenshot saved to screenshot_upload_error.png")
				return fmt.Errorf("upload button not found and file input not found for image %d: %w", i+1, err)
			}
			return fmt.Errorf("file input not found for image %d: %w", i+1, err)
		}
		fileInput = fileInput.CancelTimeout()
		
		if err := fileInput.SetFiles([]string{localPath}); err != nil {
			return fmt.Errorf("failed to set file path for image %d: %w", i+1, err)
		}
		
		// 等待并点击图片确认按钮（如 data-e2e="imageUploadConfirm-btn" 或文本为“确定”的按钮）
		log.Infof("Waiting for image %d upload and confirm dialog...", i+1)
		var clickedImgConfirm bool
		for k := 0; k < 15; k++ { // 最多等 7.5 秒
			// 自动选中已上传的缩略图以启用确认按钮
			_, _ = a.page.Eval(`() => {
				let imgs = Array.from(document.querySelectorAll('img'));
				imgs.forEach(img => {
					if (img.offsetWidth > 0 && img.offsetHeight > 0) {
						if (img.closest('[class*="header"]') || img.closest('[class*="sidebar"]') || img.closest('[class*="nav"]') || img.closest('[class*="user"]')) {
							return;
						}
						let wrapper = img.closest('[class*="item"]') || img;
						if (!wrapper.dataset.mcpClicked) {
							wrapper.click();
							wrapper.dataset.mcpClicked = "true";
						}
					}
				});
			}`)
			time.Sleep(200 * time.Millisecond)

			// 先清理标记
			_, _ = a.page.Eval(`() => {
				document.querySelectorAll('.mcp-confirm-btn').forEach(el => el.classList.remove('mcp-confirm-btn'));
			}`)

			res, err := a.page.Eval(`() => {
				let btn = document.querySelector('[data-e2e="imageUploadConfirm-btn"]') ||
				          Array.from(document.querySelectorAll('button')).find(b => {
				              let text = b.textContent ? b.textContent.trim() : '';
				              return (text === '确定' || text === '确认') && b.offsetWidth > 0;
				          });
				if (btn) {
					btn.classList.add('mcp-confirm-btn');
					return true;
				}
				return false;
			}`)

			if err == nil && res != nil && res.Value.Bool() {
				// 获取此按钮并使用安全物理点击
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
				} else {
					log.Warn("Failed to get confirm button element in Go, fallback to JS click")
					_, _ = a.page.Eval(`() => {
						let btn = document.querySelector('.mcp-confirm-btn');
						if (btn) btn.click();
					}`)
					clickedImgConfirm = true
				}

				// 清理标记
				_, _ = a.page.Eval(`() => {
					document.querySelectorAll('.mcp-confirm-btn').forEach(el => el.classList.remove('mcp-confirm-btn'));
				}`)

				// 等待弹窗在页面上消失
				log.Info("Waiting for upload confirm dialog to close...")
				dialogClosed := false
				for j := 0; j < 10; j++ { // 最多等 5 秒让弹窗关闭
					resExist, errExist := a.page.Eval(`() => {
						let btn = document.querySelector('[data-e2e="imageUploadConfirm-btn"]') ||
						          Array.from(document.querySelectorAll('button')).find(b => {
						              let text = b.textContent ? b.textContent.trim() : '';
						              return (text === '确定' || text === '确认') && b.offsetWidth > 0;
						          });
						return btn ? true : false;
					}`)
					if errExist == nil && resExist != nil && !resExist.Value.Bool() {
						log.Info("Upload confirm dialog closed successfully")
						dialogClosed = true
						break
					}
					// 补点一次
					_, _ = a.page.Eval(`() => {
						let btn = document.querySelector('[data-e2e="imageUploadConfirm-btn"]') ||
						          Array.from(document.querySelectorAll('button')).find(b => {
						              let text = b.textContent ? b.textContent.trim() : '';
						              return (text === '确定' || text === '确认') && b.offsetWidth > 0;
						          });
						if (btn) btn.click();
					}`)
					time.Sleep(500 * time.Millisecond)
				}
				if dialogClosed {
					break
				}
			}
			time.Sleep(500 * time.Millisecond)
		}
		if !clickedImgConfirm {
			log.Warnf("Image upload confirm button not found or click failed for image %d", i+1)
		}

		time.Sleep(1500 * time.Millisecond)
	}
	return nil
}

// checkCoverSlotHasImage 检查第 i 个封面插槽中是否已经有已上传的图片
func (a *ArticlePublishAction) checkCoverSlotHasImage(index int) (bool, error) {
	res, err := a.page.Timeout(3 * time.Second).Eval(`(i) => {
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
	
	// 1. 展开“发文设置”
	_, _ = a.page.Timeout(3*time.Second).Eval(`() => {
		let settingsTrigger = Array.from(document.querySelectorAll('*')).find(el => {
			let text = el.textContent ? el.textContent.trim() : '';
			return (text === '发文设置' || text === '发文设置 ∨' || text === '发文设置 ^') && el.children.length <= 1;
		});
		if (settingsTrigger) {
			settingsTrigger.click();
		}
	}`)
	time.Sleep(1 * time.Second)

	// 2. 清理已有标记和隐藏可能遮挡的元素
	a.dismissObstacles()
	_, _ = a.page.Eval(`() => {
		document.querySelectorAll('.mcp-target-to-click').forEach(el => el.classList.remove('mcp-target-to-click'));
	}`)

	// 3. JS 定位目标元素，滚动并打上临时标记
	res, err := a.page.Timeout(5 * time.Second).Eval(`(modeText) => {` + SafeScrollJS + `
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
	
	// 清理已有的临时类名并隐藏障碍物
	a.dismissObstacles()
	_, _ = a.page.Eval(`() => {
		document.querySelectorAll('.mcp-original-target').forEach(el => el.classList.remove('mcp-original-target'));
	}`)

	res, err := a.page.Timeout(5 * time.Second).Eval(`() => {` + SafeScrollJS + `
		let target = document.querySelector('.pgc-declare-original-checkbox') ||
		             document.querySelector('input[name="original"]') ||
		             document.querySelector('input[value="original"]') ||
		             Array.from(document.querySelectorAll('span, label, p')).find(el => {
		                 let text = el.textContent ? el.textContent.trim() : '';
		                 return (text === '声明原创' || text === '原创') && el.children.length === 0;
		             });
		if (target) {
			let label = target.closest('label') || target;
			scrollIntoViewSafe(label);
			label.classList.add('mcp-original-target');
			return true;
		}
		return false;
	}`)

	if err != nil || (res != nil && !res.Value.Bool()) {
		log.Warnf("Failed to locate '原创' checkbox: %v", err)
		return
	}

	// 在 Go 层面获取这个带标记的元素并执行物理点击
	clickEl, err := a.page.Timeout(5 * time.Second).Element(".mcp-original-target")
	if err == nil && clickEl != nil {
		log.Info("Clicking '原创' checkbox")
		pt, err := clickEl.Interactable()
		if err == nil {
			_ = a.page.Mouse.MoveTo(*pt)
			_ = a.page.Mouse.Down(proto.InputMouseButtonLeft, 1)
			_ = a.page.Mouse.Up(proto.InputMouseButtonLeft, 1)
			time.Sleep(1 * time.Second)
		} else {
			log.Warnf("Failed to get interactable point for '原创' checkbox, fallback to JS: %v", err)
			_, _ = clickEl.Eval("() => this.click()")
		}
	}

	// 清理临时标记
	_, _ = a.page.Eval(`() => {
		document.querySelectorAll('.mcp-original-target').forEach(el => el.classList.remove('mcp-original-target'));
	}`)
}

// setFictionDeclaration 勾选“取材网络，虚构演绎”作品声明
func (a *ArticlePublishAction) setFictionDeclaration() {
	log.Info("Attempting to set '虚构演绎' (取材网络，虚构演绎) label...")
	
	// 确保展开“发文设置”
	_, _ = a.page.Timeout(3*time.Second).Eval(`() => {
		let settingsTrigger = Array.from(document.querySelectorAll('*')).find(el => {
			let text = el.textContent ? el.textContent.trim() : '';
			return (text === '发文设置' || text === '发文设置 ∨' || text === '发文设置 ^') && el.children.length <= 1;
		});
		if (settingsTrigger) {
			settingsTrigger.click();
		}
	}`)
	time.Sleep(1 * time.Second)

	// 清理已有的临时类名并隐藏障碍物
	a.dismissObstacles()
	_, _ = a.page.Eval(`() => {
		document.querySelectorAll('.mcp-fiction-target').forEach(el => el.classList.remove('mcp-fiction-target'));
	}`)

	res, err := a.page.Timeout(5 * time.Second).Eval(`() => {` + SafeScrollJS + `
		let target = Array.from(document.querySelectorAll('span, label, div, p')).find(el => {
			if (el.children.length > 0) return false;
			let text = el.textContent ? el.textContent.trim() : '';
			return text.includes('取材网络') && text.includes('虚构演绎');
		});
		if (!target) {
			target = Array.from(document.querySelectorAll('span, label')).find(el => {
				let text = el.textContent ? el.textContent.trim() : '';
				return text.includes('取材网络') && text.includes('虚构演绎');
			});
		}
		if (target) {
			let label = target.closest('label') || target;
			scrollIntoViewSafe(label);
			label.classList.add('mcp-fiction-target');
			return true;
		}
		return false;
	}`)

	if err != nil || (res != nil && !res.Value.Bool()) {
		log.Warnf("Failed to locate '虚构演绎' checkbox: %v", err)
		return
	}

	// 在 Go 层面获取这个带标记的元素并执行物理点击
	clickEl, err := a.page.Timeout(5 * time.Second).Element(".mcp-fiction-target")
	if err == nil && clickEl != nil {
		log.Info("Clicking '虚构演绎' checkbox")
		pt, err := clickEl.Interactable()
		if err == nil {
			_ = a.page.Mouse.MoveTo(*pt)
			_ = a.page.Mouse.Down(proto.InputMouseButtonLeft, 1)
			_ = a.page.Mouse.Up(proto.InputMouseButtonLeft, 1)
			time.Sleep(1 * time.Second)
		} else {
			log.Warnf("Failed to get interactable point for '虚构演绎' checkbox, fallback to JS: %v", err)
			_, _ = clickEl.Eval("() => this.click()")
		}
	}

	// 清理临时标记
	_, _ = a.page.Eval(`() => {
		document.querySelectorAll('.mcp-fiction-target').forEach(el => el.classList.remove('mcp-fiction-target'));
	}`)
}

func (a *ArticlePublishAction) clickPublish(opts *ArticleOptions) error {
	if err := clickFirst(a.page, 3*time.Second, ArticlePublishButtonSelectors, "publish button"); err != nil {
		return err
	}
	
	// 保存点击后的截图用于调试分析
	screenshotPath := "./publish_after_first_click.png"
	time.Sleep(1 * time.Second) // 稍微等一秒再截图，防止截到空白
	_ = a.page.MustScreenshot(screenshotPath)
	log.Infof("Saved first-click screenshot to: %s", screenshotPath)

	// 如果需要定时发布，在确认发布前设置定时发布时间
	if opts != nil && opts.PublishTime != nil {
		if err := setPublishTime(a.page, opts.PublishTime); err != nil {
			log.Warnf("设置定时发布时间失败: %v", err)
		}
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

		res, err := a.page.Eval(`() => {
			// 查找所有可见按钮
			let buttons = Array.from(document.querySelectorAll('button'));
			let visibleButtons = buttons.filter(b => b.offsetWidth > 0 && b.offsetHeight > 0);
			
			// 寻找包含“确认发布”、“确认发表”、“确定”、“确认”、“发布”等文字，且不包含“预览”的按钮
			let confirmBtn = visibleButtons.find(b => {
				let text = b.textContent ? b.textContent.trim() : '';
				return (text === '确认发布' || text === '确认发表' || text === '确定' || text === '确认' || text === '发布') && !text.includes('预览');
			});
			
			if (confirmBtn) {
				confirmBtn.click();
				return JSON.stringify({
					clicked: true,
					text: confirmBtn.textContent.trim(),
					html: confirmBtn.outerHTML
				});
			}
			
			// 兜底寻找：在对话框/模态框中的最后一个按钮
			let modal = document.querySelector('.semi-modal, .byte-modal, [class*="modal"], [class*="dialog"]');
			if (modal) {
				let modalButtons = Array.from(modal.querySelectorAll('button')).filter(b => b.offsetWidth > 0 && b.offsetHeight > 0);
				if (modalButtons.length > 0) {
					let lastBtn = modalButtons[modalButtons.length - 1];
					lastBtn.click();
					return JSON.stringify({
						clicked: true,
						note: 'clicked last button in modal',
						text: lastBtn.textContent.trim(),
						html: lastBtn.outerHTML
					});
				}
			}
			
			return JSON.stringify({ clicked: false });
		}`)
		if err == nil && res != nil {
			lastJSResult = res.Value.Str()
			if strings.Contains(lastJSResult, `"clicked":true`) {
				log.Infof("Secondary publish confirmation succeeded: %s", lastJSResult)
				clickedConfirm = true
				break
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	if !clickedConfirm {
		log.Warnf("Secondary publish confirmation button not found or click failed. Last JS result: %s", lastJSResult)
	}

	time.Sleep(1 * time.Second)

	// 等待并检测发布结果，最多等待 25 秒（包含跳转时间）
	return waitForPublishResult(a.page, 25*time.Second)
}

// truncateStr 截取字符串，超出长度加省略号
func truncateStr(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
