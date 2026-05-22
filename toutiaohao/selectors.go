package toutiaohao

// 页面选择器常量

// 登录页选择器 - 多选择器回退
var LoginUsernameSelectors = []string{
	`input[placeholder*='手机号']`,
	`input[placeholder*='账号']`,
	`input[name='mobile']`,
	`input[type='tel']`,
}

var LoginPasswordSelectors = []string{
	`input[placeholder*='密码']`,
	`input[type='password']`,
	`input[name='password']`,
}

var LoginSubmitButtonSelectors = []string{
	`button[type='submit']`,
	`button.btn-login`,
	`//button[contains(., '登录')]`,
}

// 文章发布页选择器
const (
	ArticleTitleInput    = `textarea[placeholder*='请输入文章标题']`
	ArticleContentEditor = `.ProseMirror`
	ArticleCoverAdd      = `div.article-cover-add`
	ArticleCoverUpload   = `div.btn-upload-handle.upload-handler`
	ArticlePublishButton = `button.publish-btn.publish-btn-last`
)

var ArticleTitleSelectors = []string{
	ArticleTitleInput,
	`textarea[placeholder*='标题']`,
	`input[placeholder*='标题']`,
	`[contenteditable='true'][data-placeholder*='标题']`,
	`//textarea[contains(@placeholder, '标题')]`,
	`//input[contains(@placeholder, '标题')]`,
}

var ArticleContentSelectors = []string{
	ArticleContentEditor,
	`[contenteditable='true']`,
	`div[role='textbox']`,
	`textarea[placeholder*='正文']`,
	`textarea[placeholder*='内容']`,
}

var ArticleCoverAddSelectors = []string{
	ArticleCoverAdd,
	`[class*='cover'][class*='add']`,
	`//div[contains(., '封面') and (contains(@class, 'add') or contains(@class, 'cover'))]`,
	`//button[contains(., '封面')]`,
}

var ArticleCoverUploadSelectors = []string{
	ArticleCoverUpload,
	`[class*='upload-handler']`,
	`button[class*='upload']`,
	`//button[contains(., '上传')]`,
	`//div[contains(., '上传') and contains(@class, 'upload')]`,
}

var ArticlePublishButtonSelectors = []string{
	ArticlePublishButton,
	`button[class*='publish']`,
	`//button[normalize-space()='发布']`,
	`//button[contains(., '发布')]`,
}

// 微头条发布页选择器 - 多选择器回退
var MicroEditorSelectors = []string{
	`.ProseMirror`,
	`[contenteditable='true']`,
	`textarea`,
	`div[role='textbox']`,
}

var MicroPublishButtonSelectors = []string{
	`button.publish-content`,
	`//button[contains(., '发布')]`,
	`button[class*='publish']`,
}
