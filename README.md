# 头条号 MCP & HTTP 自动化运营服务端 (Toutiao MCP Server)

[![Go Version](https://img.shields.io/badge/Go-1.20%2B-blue.svg)](https://go.dev/)
[![Model Context Protocol](https://img.shields.io/badge/MCP-1.0-orange.svg)](https://modelcontextprotocol.io/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

基于 [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) 与 Go 语言实现的头条号创作者后台自动化运营工具包。支持作为标准 MCP 服务端直接接入 AI 客户端（如 Cursor、DeepSeek、Claude Desktop、Claude Code），同时也暴露了标准的 RESTful HTTP 接口，实现自媒体发文、数据分析与内容管理的自动化。

---

## 🚀 核心特性

- **双通道服务架构**：同一端口（默认 `8080`）下，同时提供符合标准 MCP 协议的 JSON-RPC 路由（`/mcp`）与面向传统 Webhooks 的 RESTful HTTP API。
- **文章在线二次修改与安全测试沙盒**：支持对已发布或草稿状态的文章进行无残留二次修改。新支持 `SaveAsDraft` 选项（仅保存草稿不发布），支持纯草稿箱安全测试，测试数据不污染真实主页，且测试完毕后能通过浏览器物理清除。智能跳过非必要封面重传以防超时；引入 React onChange 底层强力驱动与 3s 延迟平息期以彻底防止 React 异步重绘回滚；在最终发布前执行“一致性二次固化校验与强行覆盖”，保证提交数据 100% 固化；物理兼容“发布”确认弹窗。
- **登录态自愈机制**：支持本地 `cookies.json` 的自动装载与生命周期管理。若会话过期，程序会在有头模式下自动弹窗等待扫码登录，登录成功后自动更新并捕获凭证回写。
- **Markdown 智能排版引擎**：
  - 自动将正文解析分割为文本块与图片块，由浏览器控制完成图文交替混排。
  - 支持运行时相对图片路径（如 `./testdata/test.png`）的自动解析与 Chrome 绝对路径转换上传。
  - 正文插图与封面图统一通过 Chrome 原生文件选择器上传，规避隐藏 file input 直写导致的“无效图片数据”问题。
  - 具备排版格式符强力清洗，自动滤除正文残留的 Markdown 标记符号。
- **合规合规合规**：支持在发文设置中，自动物理点击并勾选“取材网络，虚构演绎”的作品声明，降低封禁风险。
- **运营分析报告**：自动分析创作者后台的数字指标与卡片，生成格式化账户概览及统计数据报告。

---

## 🛠️ 技术栈与依赖前置

| 组件 | 选型与依赖 |
|------|------|
| **编程语言** | Go 1.20+ |
| **MCP 协议 SDK** | `github.com/modelcontextprotocol/go-sdk` |
| **浏览器自动化** | `github.com/go-rod/rod` (高容错 Chrome 控制库) |
| **HTTP 框架** | `github.com/gin-gonic/gin` (高性能 HTTP 引擎) |
| **系统依赖** | **运行环境必须已安装 Chrome 或 Chromium 浏览器** (自动化控制依赖) |

---

## ⚙️ 快速开始

### 1. 项目编译

```bash
go build -o toutiaohao-server .
```

### 2. 启动服务

```bash
# 启动混合服务（默认监听 8080 端口，包含 MCP 路由及 HTTP 接口）
./toutiaohao-server

# 以标准 Stdio 管道传输模式运行 (专供 AI 本地集成)
./toutiaohao-server -stdio

# 指定自定义端口启动混合服务
./toutiaohao-server -port 9000
```

### 3. 初始化登录态 (Cookie)
1. 首次启动服务并调用发文或查询功能时，若本地 `cookies.json` 不存在，程序会自动弹出一个 Chrome 浏览器窗口。
2. 请在弹出的浏览器中手动进行**扫码登录**或**短信登录**。
3. 登录成功后，终端将输出 `检测到登录成功！`，程序会在后台自动捕获最新的 Session Cookie 并保存为项目根目录下的 `cookies.json`。
4. 后续调用将全部转为无头（Headless）静默运行，无需人工干预。

---

## 🤖 AI 协同与 MCP 配置

为了使 AI 客户端（如 Cursor、Claude Desktop）更高效、稳定地集成，建议使用本地无端口占用的 **Stdio 管道传输模式**（添加 `-stdio` 参数）。这能避免本地端口占用与冲突，且比 HTTP/SSE 传输方式具有更高的响应速度。

### 1. Cursor 配置
在 Cursor 中打开 `Settings -> Features -> MCP`，添加一个新的 MCP 服务：
- **Name**: `toutiao`
- **Type**: `command`
- **Command**: `/path/to/your/toutiaohao-server -stdio`

### 2. Claude Desktop 配置
编辑您的 `config.json`（通常在 `~/Library/Application Support/Claude/claude_desktop_config.json`）：

```json
{
  "mcpServers": {
    "toutiao-mcp": {
      "command": "/path/to/your/toutiaohao-server",
      "args": ["-stdio"]
    }
  }
}
```

> [!TIP]
> 本项目已针对 AI 工具进行全方位的 AI-Friendly 深度优化。项目根目录下的 `AGENTS.md` 包含 AI 进行开发时的详细设计心智和约束规范。

---

## 🔗 HTTP 接口列表 (REST API)

| 请求方法 | 请求路径 | 功能说明 |
|:---:|---|---|
| **POST** | `/api/v1/publish/article` | 发布图文文章（支持 Markdown 图片分割与插图排版） |
| **POST** | `/api/v1/publish/micro` | 发布微头条（支持最多 9 张插图与话题标签） |
| **POST** | `/api/v1/publish/micro/draft` | 保存微头条草稿 |
| **GET** | `/api/v1/articles` | 获取文章列表（支持 `published/draft/review` 状态筛选） |
| **POST** | `/api/v1/articles/update` | 修改/更新已有图文文章 |
| **POST** | `/api/v1/articles/delete` | 物理删除文章或删除草稿 |
| **GET** | `/api/v1/analytics/overview` | 账户整体运营数据（粉丝、阅读量、展现量指标） |
| **GET** | `/api/v1/analytics/article` | 单篇文章阅读与交互明细统计 |
| **GET** | `/api/v1/analytics/report` | 自动生成运营日报/周报/月报 |
| **GET** | `/health` | 健康检查端点 |

---

## 📂 项目结构

```
toutiaohao-mcp-server/
├── main.go                 # 服务主入口
├── app_server.go           # 混合服务器引擎（聚合路由、MCP 与 HTTP Server）
├── mcp_server.go           # MCP Protocol 服务端初始化与 Tools 映射注册
├── mcp_handlers.go         # MCP 工具的具体分发处理器（参数校验与业务调度）
├── handlers_api.go         # REST API 接口的具体处理器
├── routes.go               # Gin HTTP 路由注册表
├── middleware.go           # 跨域 (CORS) 过滤与全局 Panic 拦截中间件
├── service.go              # 核心业务服务层（抽象调用底层自动化组件）
├── types.go                # 全局通用业务数据结构定义
├── configs/                # 静态配置模块
│   ├── urls.go             # 头条号各种导航页面 URL 与 API 接口端点
│   ├── content.go          # 头条号文章字数、图片大小限制
│   └── browser.go          # 浏览器性能限流及 Chrome 路径配置
├── cookies/                # 凭证存储与读取模块
│   └── cookies.go          # Cookier 接口及本地文件 FileCookieStore 实现
├── browser/                # 浏览器控制封装模块
│   └── browser.go          # go-rod 浏览器实例初始化及 Cookie 注入
└── toutiaohao/                # 【核心业务组件】
    ├── login.go            # 扫码登录、状态轮询与 Cookie 自动保存
    ├── dom.go              # 高容错 DOM 寻址、页面平滑滚动与障碍物清除
    ├── selectors.go        # 头条号创作者平台各个按钮/输入框的 CSS 选择器
    ├── publish_article.go  # 发图文（Markdown 解析、图片自动上传排版、虚构声明勾选）
    ├── publish_micro.go    # 发布微头条
    ├── draft_micro.go      # 保存微头条草稿
    ├── article_list.go     # 内容列表拉取与管理
    └── analytics.go        # 创作者运营数据面板解析与抓取
```

---

## 🧪 自动化测试与验证

### 1. 静态编译自检
```bash
go build ./...
```

### 2. 自动化图文发文集成测试
测试需要本地存在有效的 `cookies.json`（需要先启动服务扫码登录一次生成）。

在测试时，程序会准备临时测试图片，验证封面上传、定时发布弹窗和草稿保存路径。图片净化后的中转文件统一写入项目根目录 `.tmp/`：

```bash
cd toutiaohao
go test -v -run TestPublishArticleManual
```

### 3. 正文插图上传专项测试

此测试专门覆盖 Markdown 正文中的 `![图片](path)` 插图路径，确保浏览器通过 Chrome 原生文件选择器接收文件，并且图片上传确认弹窗能正常关闭：

```bash
cd toutiaohao
go test -v -run TestPublishArticleBodyImageManual
```

预期日志中应出现：

```text
Chrome 文件选择器已接收文件
正文图片上传确认弹窗已成功关闭
```

### 4. 自动化修改文章集成测试 (安全沙盒闭环)
此测试自动开展“发布临时新文章 -> 从文章列表提取新文章 ID -> 物理修改新文章 -> 自动删除临时新文章”的闭环测试，确保高容错性且不损耗线上老文章的宝贵修改额度，无任何脏数据残留：

```bash
cd toutiaohao
go test -v -run TestUpdateArticleManual
```

### 图片上传实现约定

- 所有本地/下载/净化后的临时图片必须生成在项目根目录 `.tmp/` 下。
- 正文插图和封面图上传必须使用 go-rod `HandleFileDialog()` 拦截 Chrome 原生文件选择器，再物理点击当前可见上传弹窗里的“本地上传”按钮。
- 不要直接对隐藏的 `input[type=file]` 调用 `SetFiles`。该方式可能产生本地 blob 预览，但服务端实际收到约 210 字节的损坏请求体，最终显示“无效图片数据”。
- `/spice/image`、`upload_source=` 和 `multipart/form-data` 图片上传请求必须绕过 `HijackRequests` 的 `LoadResponse` 代理，直接 `ContinueRequest` 原样放行。
- 如果图片确认弹窗没有关闭，应保存错误截图并返回错误，避免缺图内容继续发布。

### 草稿删除实现约定

- 删除接口会先调用头条 HTTP 删除 API，但不信任仅返回 success 的结果；必须重新查询列表确认文章消失。
- 草稿删除失败时会回退到浏览器自动化：用 `rodBrowser.MustPage("https://mp.toutiao.com/profile_v4/graphic/articles?status=draft")` 一步到位打开草稿列表，注入 Cookie 后 `Reload()`，然后在当前页面定位标题/ID 匹配的草稿卡片。
- 不要创建空白页后再 `page.Navigate()` 到草稿箱；该两步式导航可能导致 SPA 数据请求未携带 Cookie，表现为 URL 正确但列表卡片为空。
- 如果当前 URL 已经包含 `status=draft` 或 `status=1`，说明页面已经在草稿列表路由，不要再次点击左侧“草稿箱”；直接等待列表渲染并定位目标草稿，避免 SPA 路由重入导致卡片区空白。
- 草稿删除不依赖“更多”菜单或编辑页删除按钮；应勾选目标草稿行的真实复选框，再点击批量操作栏中的“删除”，最后处理确认弹窗。
- 复选框、批量删除按钮和确认按钮都必须只点击真实可见的小按钮本体，不要点击 `.pgc-content` 等外层容器。点击前需滚入视口中心，并使用 rod 鼠标坐标物理点击。
- 若仍失败，会保存 `screenshot_delete_draft_not_found.png`、`screenshot_delete_draft_checkbox_error.png`、`screenshot_delete_draft_batch_error.png` 或 `screenshot_delete_draft_confirm_error.png` 以便分析。

---

## 📄 开源许可证

本项目基于 [MIT License](LICENSE) 许可证开源。请在使用时注意凭证文件的安全性，严禁将包含敏感会话 Token 的 `cookies.json` 上传至任何公共代码仓库中。
