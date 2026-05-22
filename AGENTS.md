# AGENTS.md

## 1. 项目概述
本仓库是一个基于 Go 语言与 Chrome 浏览器自动化技术（go-rod）实现的今日头条（头条号）自动化运营 MCP 服务端及 HTTP 服务。
- **核心定位**：自媒体发文、数据监控与分析抓取的自动化工具包。
- **架构模式**：双通道服务。支持作为标准的 RESTful HTTP 服务运行，或作为标准 MCP (Model Context Protocol) 服务端接入 AI 客户端（如 Cursor、DeepSeek、Claude Desktop），使 AI 可直接调用发文、获取数据等功能。
- **技术栈**：Go 1.20+、go-rod (浏览器自动化)、RESTful API、MCP JSON-RPC 协议。

---

## 2. 快速命令与环境变量

### 常用命令
```bash
# 项目编译
go build -o toutiaohao-server .

# 启动服务 (在默认的 8080 端口上同时提供 MCP 服务与 REST HTTP 服务)
./toutiaohao-server

# 指定自定义端口启动服务
./toutiaohao-server -port 9000

# 执行指定集成测试（发图文、存草稿逻辑验证）
cd toutiaohao && go test -v -run TestPublishArticleManual
```

### 凭证配置
- 用户的登录态 Cookie 统一存储在项目根目录的 `cookies.json` 文件中。
- 环境变量 `TOUTIAOHAO_COOKIES_PATH` 可以指定自定义的 Cookie 路径（若不指定则回退到当前目录或项目根目录下的 `cookies.json`）。

---

## 3. 项目架构与目录说明

```
project-root/
  ├── main.go                 # 服务主入口 (解析启动模式，拉起 MCP 或 HTTP 服务)
  ├── mcp_server.go           # MCP 协议服务端实现 (定义并映射 JSON-RPC 消息)
  ├── mcp_handlers.go         # MCP 工具的具体分发处理器
  ├── handlers_api.go         # 传统 HTTP 接口的具体处理器
  ├── app_server.go           # RESTful HTTP 服务引擎管理
  ├── routes.go               # HTTP API 路由表
  ├── middleware.go           # HTTP 跨域与鉴权中间件
  ├── service.go              # 服务逻辑封装层 (衔接 API/MCP 接口与 toutiao 核心业务)
  ├── types.go                # 全局通用数据实体
  ├── cookies.json            # [被.gitignore屏蔽] 头条登录态 Cookie 数据
  ├── browser/                # 浏览器驱动与控制模块
  │   └── browser.go          # go-rod 浏览器实例初始化及 Cookie 自动挂载
  ├── configs/                # 全局静态配置文件
  │   ├── browser.go          # 浏览器运行限制及 Chrome 路径
  │   ├── content.go          # 头条文章长度、分类静态限制
  │   └── urls.go             # 头条号各种页面导航及内部 API 路由表
  ├── cookies/                # 凭证存储与管理
  │   └── cookies.go          # 实现 Cookier 接口的本地文件存储器
  └── toutiaohao/                # 【核心业务模块】操作头条创作者后台的自动化实现
      ├── login.go            # 扫码登录、登录状态监控及 Cookie 自动回写
      ├── dom.go              # 高容错的页面 DOM 寻址、滑动、错误截图及障碍物消除
      ├── selectors.go        # 头条号创作者平台各个按钮/输入框 of CSS 元素选择器
      ├── publish_article.go  # 核心：发图文（Markdown 解析、图片自动上传排版、虚构声明勾选）
      ├── publish_micro.go    # 核心：发微头条
      ├── draft_micro.go      # 核心：存微头条草稿
      ├── article_list.go     # 核心：获取文章列表
      └── analytics.go        # 核心：抓取创作者后台数据并生成分析报告
```

---

## 4. 核心发文与排版规则 (硬性约定)

AI 助手在此项目中编写代码或执行自动化修改时，必须严格遵守以下约定：

1. **零硬编码绝对路径**：
   - 严禁在代码中写入任何包含开发机器用户名（例如 `/Users/username/...`）的绝对路径。
   - 所有本地图片、截图均使用相对路径表示。在传入浏览器上传组件之前，必须在 Go 层面通过 `filepath.Abs(path)` **运行时动态转换为绝对路径**（因底层 Chrome 只接受本地绝对路径），以此达到“代码脱敏”与“高可用性”的统一。
   - 截图调试保存路径统一设为相对路径（如 `./screenshot_analytics.png`），以防泄露系统物理路径信息。

2. **凭证隔离与安全性**：
   - `cookies.json` 包含用户的敏感 Session 凭证，严禁将其上传至代码仓库中（已被 `.gitignore` 规则拦截，开发中切勿破坏此规则）。
   - 在程序逻辑中，仅在用户登录成功后才从浏览器内存捕获 Cookie 并保存至本地文件，严禁通过日志打印出 Cookie 的敏感内容。

3. **测试解耦与清理机制**：
   - 在编写集成测试（如 `publish_integration_test.go`）时，若需要测试图文发文插图，**不得**依赖本地已有的特定物理图片。
   - 必须在测试运行开始前通过 Go 代码自动在本地动态生成一个临时测试图片（例如生成包含 Dummy 伪 PNG 数据的 `./testdata/test_cloud_computing.png`），并在测试结束时（通过 `defer`）物理清理删除该临时目录，保证测试在任何人电脑上都能做到“一键跑通、测试后无垃圾留存”。

4. **排版清洗过滤**：
   - 今日头条的富文本编辑器并不直接支持完整的 Markdown 标记。因此，发文时必须在 `insertTextAtCursor` 中通过 `mdToCleanText` 函数过滤并剔除正文中的 `####`、`**` 等 Markdown 标记，确保最终页面排版清晰整洁。

5. **虚构演绎合规性声明**：
   - 对于故事型或非实时新闻稿的演绎发文，必须支持在发布前通过 `setFictionDeclaration` 物理勾选作品声明中的“取材网络，虚构演绎”复选框，合规第一。

---

## 5. 本地开发与测试流程

AI 助手在修改项目后需进行完整的自我闭环验证，流程如下：

1. **静态代码编译检查**：
   在项目根目录下执行编译，确认无任何语法错误：
   ```bash
   go build ./...
   ```

2. **发文自动化流验证**：
   在 `toutiao/` 目录下执行集成测试，并确保当前终端可以拉起 Chrome 浏览器：
   ```bash
   go test -v -run TestPublishArticleManual
   ```
   **预期验证结果**：
   - 测试能够在控制台输出 `Loading cookies from ../cookies.json`。
   - 自动生成 `testdata/test_cloud_computing.png` 临时图片。
   - 浏览器自动运行输入标题、自动上传测试图片、模拟排版、自动勾选虚构演绎声明并保存草稿。
   - 测试结束后自动输出 `PASS`，且 `testdata/` 目录被物理清除干净。
