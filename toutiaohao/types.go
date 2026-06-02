package toutiaohao

// LoginRequest 登录请求
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse 登录响应
type LoginResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// LoginStatusResponse 登录状态响应
type LoginStatusResponse struct {
	LoggedIn bool   `json:"logged_in"`
	Message  string `json:"message"`
}

// PublishResult 发布/更新文章结果
type PublishResult struct {
	Success        bool   `json:"success"`
	Message        string `json:"message"`
	ArticleID      string `json:"article_id,omitempty"`
	CoverStatus    string `json:"cover_status"`
	OriginalStatus string `json:"original_status"`
}

