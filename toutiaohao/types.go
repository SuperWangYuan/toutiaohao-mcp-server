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
