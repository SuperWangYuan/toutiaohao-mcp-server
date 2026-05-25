package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	log "github.com/sirupsen/logrus"
)

// InitMCPServer 初始化 MCP Server 并注册工具
func InitMCPServer(appServer *AppServer) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "toutiao-mcp-server",
		Version: "1.0.0",
	}, &mcp.ServerOptions{
		Capabilities: &mcp.ServerCapabilities{
			Tools: &mcp.ToolCapabilities{},
		},
	})

	registerTools(server, appServer)
	return server
}

// LoginArgs 登录工具参数
type LoginArgs struct {
	Username string `json:"username" jsonschema_description:"头条账号（手机号）"`
	Password string `json:"password" jsonschema_description:"账号密码"`
}

// CheckLoginStatusArgs 检查登录状态参数（无参数）
type CheckLoginStatusArgs struct{}

// DeleteCookiesArgs 删除 Cookie 参数（无参数）
type DeleteCookiesArgs struct{}

// PublishArticleArgs 文章发布参数
type PublishArticleArgs struct {
	Title       string      `json:"title" jsonschema_description:"文章标题（最多100字）"`
	Content     string      `json:"content" jsonschema_description:"文章正文内容。支持：1. 纯文本内容。2. 包含插图的内容：使用 Markdown 格式 of 图片标签 '![图片描述](本地绝对路径)' 插入本地图片。系统会自动提取图片，按顺序逐个上传并在对应的段落位置插入。例如：'第一段内容。\\n\\n![插图](/path/to/img.png)\\n\\n第二段内容。'"`
	Images      []string    `json:"images,omitempty" jsonschema_description:"封面图片备选或文章关联图片路径列表"`
	Tags        []string    `json:"tags,omitempty" jsonschema_description:"标签列表（如活动话题：移动云智能新空间）"`
	Category    string      `json:"category,omitempty" jsonschema_description:"文章分类（如科技）"`
	CoverImage  string      `json:"cover_image,omitempty" jsonschema_description:"封面图片路径"`
	Original    bool        `json:"original,omitempty" jsonschema_description:"是否声明原创"`
	Fiction     bool        `json:"fiction,omitempty" jsonschema_description:"是否声明作品取材网络、虚构演绎以防范版权/姓名权争议"`
	PublishTime interface{} `json:"publish_time,omitempty" jsonschema_description:"定时发布时间（支持 Unix 时间戳或 YYYY-MM-DD HH:mm 格式的字符串）"`
}

// UpdateArticleArgs 文章修改参数
type UpdateArticleArgs struct {
	ArticleID   string      `json:"article_id" jsonschema_description:"要修改的文章或草稿的 ID (即 URL 中的 pgc_id)"`
	Title       string      `json:"title,omitempty" jsonschema_description:"修改后的文章标题（最多100字，不修改则留空）"`
	Content     string      `json:"content,omitempty" jsonschema_description:"修改后的文章正文内容。支持：1. 纯文本内容。2. 包含插图的内容：使用 Markdown 格式 of 图片标签 '![图片描述](本地绝对路径)' 插入本地图片。不修改则留空。"`
	Images      []string    `json:"images,omitempty" jsonschema_description:"封面图片备选或文章关联图片路径列表"`
	CoverImage  string      `json:"cover_image,omitempty" jsonschema_description:"封面图片路径"`
	Original    bool        `json:"original,omitempty" jsonschema_description:"是否声明原创"`
	Fiction     bool        `json:"fiction,omitempty" jsonschema_description:"是否声明作品取材网络、虚构演绎以防范版权/姓名权争议"`
	PublishTime interface{} `json:"publish_time,omitempty" jsonschema_description:"定时发布时间（支持 Unix 时间戳或 YYYY-MM-DD HH:mm 格式的字符串）"`
}

// GetArticleListArgs 文章列表参数
type GetArticleListArgs struct {
	Page     int    `json:"page,omitempty" jsonschema_description:"页码（默认1）"`
	PageSize int    `json:"page_size,omitempty" jsonschema_description:"每页数量（默认20）"`
	Status   string `json:"status,omitempty" jsonschema_description:"状态筛选：all/published/draft/review（默认all）"`
}

// DeleteArticleArgs 删除文章参数
type DeleteArticleArgs struct {
	ArticleID string `json:"article_id" jsonschema_description:"文章ID"`
}

// GetArticleStatsArgs 文章统计参数
type GetArticleStatsArgs struct {
	ArticleID string `json:"article_id" jsonschema_description:"文章ID"`
}

// GenerateReportArgs 报告生成参数
type GenerateReportArgs struct {
	ReportType string `json:"report_type,omitempty" jsonschema_description:"报告类型：daily/weekly/monthly（默认weekly）"`
}

// GetAccountOverviewArgs 账户概览参数（无参数）
type GetAccountOverviewArgs struct{}

// PublishMicroPostArgs 微头条发布参数
type PublishMicroPostArgs struct {
	Content     string      `json:"content" jsonschema_description:"微头条正文内容（最多2000字）"`
	Images      []string    `json:"images,omitempty" jsonschema_description:"图片路径列表（最多9张，支持本地路径和HTTP URL）"`
	Topic       string      `json:"topic,omitempty" jsonschema_description:"话题标签（如：AI工具）"`
	PublishTime interface{} `json:"publish_time,omitempty" jsonschema_description:"定时发布时间（支持 Unix 时间戳或 YYYY-MM-DD HH:mm 格式的字符串）"`
}

// SaveMicroDraftArgs 微头条草稿保存参数
type SaveMicroDraftArgs struct {
	Content string   `json:"content" jsonschema_description:"微头条正文内容（最多2000字）"`
	Images  []string `json:"images,omitempty" jsonschema_description:"图片路径列表（最多9张）"`
	Topic   string   `json:"topic,omitempty" jsonschema_description:"话题标签"`
}

// registerTools 注册所有 MCP 工具
func registerTools(server *mcp.Server, appServer *AppServer) {
	registerAuthTools(server, appServer)
	registerMicroTools(server, appServer)
	registerArticleTools(server, appServer)
	registerManageTools(server, appServer)
	registerAnalyticsTools(server, appServer)
	log.Info("MCP tools registered")
}

// registerAuthTools 注册认证管理工具
func registerAuthTools(server *mcp.Server, appServer *AppServer) {
	// login_with_credentials
	mcp.AddTool(server, &mcp.Tool{
		Name:        "login_with_credentials",
		Description: "使用账号密码启动登录流程。浏览器将自动打开并尝试填写账密，如触发验证码需人工补充完成。",
		Annotations: &mcp.ToolAnnotations{
			DestructiveHint: boolPtr(true),
		},
	}, withPanicRecovery("login_with_credentials",
		func(ctx context.Context, req *mcp.CallToolRequest, args LoginArgs) (*mcp.CallToolResult, any, error) {
			argsMap := map[string]interface{}{
				"username": args.Username,
				"password": args.Password,
			}
			result := appServer.handleLogin(ctx, argsMap)
			return convertToMCPResult(result), nil, nil
		}))

	// check_login_status
	mcp.AddTool(server, &mcp.Tool{
		Name:        "check_login_status",
		Description: "检查当前登录状态",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
	}, withPanicRecovery("check_login_status",
		func(ctx context.Context, req *mcp.CallToolRequest, args CheckLoginStatusArgs) (*mcp.CallToolResult, any, error) {
			result := appServer.handleCheckLoginStatus(ctx, nil)
			return convertToMCPResult(result), nil, nil
		}))

	// delete_cookies
	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_cookies",
		Description: "删除本地 Cookie，重置登录状态",
		Annotations: &mcp.ToolAnnotations{
			DestructiveHint: boolPtr(true),
		},
	}, withPanicRecovery("delete_cookies",
		func(ctx context.Context, req *mcp.CallToolRequest, args DeleteCookiesArgs) (*mcp.CallToolResult, any, error) {
			result := appServer.handleDeleteCookies(ctx, nil)
			return convertToMCPResult(result), nil, nil
		}))
}

// registerMicroTools 注册微头条相关工具
func registerMicroTools(server *mcp.Server, appServer *AppServer) {
	// publish_micro_post
	mcp.AddTool(server, &mcp.Tool{
		Name:        "publish_micro_post",
		Description: "发布微头条。支持纯文本和图文内容，图片最多9张。",
		Annotations: &mcp.ToolAnnotations{
			DestructiveHint: boolPtr(true),
		},
	}, withPanicRecovery("publish_micro_post",
		func(ctx context.Context, req *mcp.CallToolRequest, args PublishMicroPostArgs) (*mcp.CallToolResult, any, error) {
			argsMap := map[string]interface{}{
				"content":      args.Content,
				"images":       args.Images,
				"topic":        args.Topic,
				"publish_time": args.PublishTime,
			}
			result := appServer.handlePublishMicroPost(ctx, argsMap)
			return convertToMCPResult(result), nil, nil
		}))

	// save_micro_post_draft
	mcp.AddTool(server, &mcp.Tool{
		Name:        "save_micro_post_draft",
		Description: "保存微头条草稿",
		Annotations: &mcp.ToolAnnotations{
			DestructiveHint: boolPtr(true),
		},
	}, withPanicRecovery("save_micro_post_draft",
		func(ctx context.Context, req *mcp.CallToolRequest, args SaveMicroDraftArgs) (*mcp.CallToolResult, any, error) {
			argsMap := map[string]interface{}{
				"content": args.Content,
				"images":  args.Images,
				"topic":   args.Topic,
			}
			result := appServer.handleSaveMicroPostDraft(ctx, argsMap)
			return convertToMCPResult(result), nil, nil
		}))
}

// registerArticleTools 注册文章发布工具
func registerArticleTools(server *mcp.Server, appServer *AppServer) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "publish_article",
		Description: "发布今日头条图文文章。\n支持两种发布模式：\n1. 发布纯文本文章：直接在 content 中填写纯文本内容。\n2. 发布带插图文章（插入图片）：在 content 中图片应该插入的位置，以 Markdown 语法 `![图片描述](图片本地绝对路径)` 指定。系统将自动解析标签、将图片文件上传并以图文交替混排方式精准插入到对应位置。\n注：图片路径必须为本地绝对路径。支持封面模式设置（单图/三图/无封面）、标签/话题设置、分类设置以及声明原创。",
		Annotations: &mcp.ToolAnnotations{
			DestructiveHint: boolPtr(true),
		},
	}, withPanicRecovery("publish_article",
		func(ctx context.Context, req *mcp.CallToolRequest, args PublishArticleArgs) (*mcp.CallToolResult, any, error) {
			argsMap := map[string]interface{}{
				"title":        args.Title,
				"content":      args.Content,
				"images":       args.Images,
				"tags":         args.Tags,
				"category":     args.Category,
				"cover_image":  args.CoverImage,
				"original":     args.Original,
				"fiction":      args.Fiction,
				"publish_time": args.PublishTime,
			}
			result := appServer.handlePublishArticle(ctx, argsMap)
			return convertToMCPResult(result), nil, nil
		}))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_article",
		Description: "修改今日头条已发表的文章或草稿。需要提供 `article_id`。如果标题、正文等字段不传入，则保留原内容。如果修改了正文，也会自动按规则重新排版及可选重新设置封面。",
		Annotations: &mcp.ToolAnnotations{
			DestructiveHint: boolPtr(true),
		},
	}, withPanicRecovery("update_article",
		func(ctx context.Context, req *mcp.CallToolRequest, args UpdateArticleArgs) (*mcp.CallToolResult, any, error) {
			argsMap := map[string]interface{}{
				"article_id":   args.ArticleID,
				"title":        args.Title,
				"content":      args.Content,
				"images":       args.Images,
				"cover_image":  args.CoverImage,
				"original":     args.Original,
				"fiction":      args.Fiction,
				"publish_time": args.PublishTime,
			}
			result := appServer.handleUpdateArticle(ctx, argsMap)
			return convertToMCPResult(result), nil, nil
		}))
}

// registerManageTools 注册内容管理工具
func registerManageTools(server *mcp.Server, appServer *AppServer) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_article_list",
		Description: "获取内容列表，支持按状态筛选（all/published/draft/review）",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
	}, withPanicRecovery("get_article_list",
		func(ctx context.Context, req *mcp.CallToolRequest, args GetArticleListArgs) (*mcp.CallToolResult, any, error) {
			argsMap := map[string]interface{}{
				"page":      float64(args.Page),
				"page_size": float64(args.PageSize),
				"status":    args.Status,
			}
			result := appServer.handleGetArticleList(ctx, argsMap)
			return convertToMCPResult(result), nil, nil
		}))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_article",
		Description: "删除文章或草稿",
		Annotations: &mcp.ToolAnnotations{
			DestructiveHint: boolPtr(true),
		},
	}, withPanicRecovery("delete_article",
		func(ctx context.Context, req *mcp.CallToolRequest, args DeleteArticleArgs) (*mcp.CallToolResult, any, error) {
			argsMap := map[string]interface{}{
				"article_id": args.ArticleID,
			}
			result := appServer.handleDeleteArticle(ctx, argsMap)
			return convertToMCPResult(result), nil, nil
		}))
}

// registerAnalyticsTools 注册数据分析工具
func registerAnalyticsTools(server *mcp.Server, appServer *AppServer) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_account_overview",
		Description: "获取账户数据概览（粉丝数、阅读量、获赞等）",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
	}, withPanicRecovery("get_account_overview",
		func(ctx context.Context, req *mcp.CallToolRequest, args GetAccountOverviewArgs) (*mcp.CallToolResult, any, error) {
			result := appServer.handleGetAccountOverview(ctx, nil)
			return convertToMCPResult(result), nil, nil
		}))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_article_stats",
		Description: "获取指定文章的统计数据（阅读、点赞、评论、转发）",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
	}, withPanicRecovery("get_article_stats",
		func(ctx context.Context, req *mcp.CallToolRequest, args GetArticleStatsArgs) (*mcp.CallToolResult, any, error) {
			argsMap := map[string]interface{}{
				"article_id": args.ArticleID,
			}
			result := appServer.handleGetArticleStats(ctx, argsMap)
			return convertToMCPResult(result), nil, nil
		}))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "generate_report",
		Description: "生成分析报告（支持日报、周报、月报）",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
	}, withPanicRecovery("generate_report",
		func(ctx context.Context, req *mcp.CallToolRequest, args GenerateReportArgs) (*mcp.CallToolResult, any, error) {
			argsMap := map[string]interface{}{
				"report_type": args.ReportType,
			}
			result := appServer.handleGenerateReport(ctx, argsMap)
			return convertToMCPResult(result), nil, nil
		}))
}

func boolPtr(b bool) *bool { return &b }

// NewMCPHTTPHandler 创建 MCP HTTP Handler
func NewMCPHTTPHandler(server *mcp.Server) http.Handler {
	return mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{
		JSONResponse: true,
	})
}

// withPanicRecovery 泛型 panic 恢复包装器
func withPanicRecovery[T any](
	toolName string,
	handler func(context.Context, *mcp.CallToolRequest, T) (*mcp.CallToolResult, any, error),
) func(context.Context, *mcp.CallToolRequest, T) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args T) (*mcp.CallToolResult, any, error) {
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("Panic in tool %s: %v", toolName, r)
			}
		}()

		result, extra, err := handler(ctx, req, args)
		if result == nil && err != nil {
			return nil, nil, err
		}

		return result, extra, err
	}
}

// convertToMCPResult 将内部 MCPToolResult 转换为 MCP SDK 格式
func convertToMCPResult(result *MCPToolResult) *mcp.CallToolResult {
	if result == nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "No result"}},
			IsError: true,
		}
	}

	var contents []mcp.Content
	for _, c := range result.Content {
		switch c.Type {
		case "text":
			contents = append(contents, &mcp.TextContent{Text: c.Text})
		case "image":
			contents = append(contents, &mcp.ImageContent{Data: []byte(c.Data), MIMEType: c.MimeType})
		default:
			contents = append(contents, &mcp.TextContent{Text: fmt.Sprintf("[unknown type: %s]", c.Type)})
		}
	}

	return &mcp.CallToolResult{
		Content: contents,
		IsError: result.IsError,
	}
}
