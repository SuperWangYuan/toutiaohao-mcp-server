# AGENTS.md

## 1. 项目概述
本仓库是一个基于 Go 语言与 Chrome 浏览器自动化技术（go-rod）实现的今日头条（头条号）自动化运营 MCP 服务端及 HTTP 服务。
- **核心定位**：自媒体发文、数据监控与分析抓取的自动化工具包。
- **架构模式**：三通道服务。支持作为标准的 RESTful HTTP 服务运行，作为标准 MCP (Model Context Protocol) 混合服务运行，或者直接通过 Stdio 管道传输协议无端口占用启动运行（专为 AI 客户端无缝本地接入设计，如 Cursor、Claude Desktop）。
- **技术栈**：Go 1.20+、go-rod (浏览器自动化)、RESTful API、MCP JSON-RPC 协议。

---

## 2. 快速命令与环境变量

### 常用命令
```bash
# 项目编译
go build -o toutiaohao-server .

# 启动混合服务 (在默认的 8080 端口上同时提供 MCP 服务与 REST HTTP 服务)
./toutiaohao-server

# 以无端口占用的标准 Stdio 管道传输模式运行 (最适合 Cursor、Claude Desktop 等本地 AI 工具集成)
./toutiaohao-server -stdio

# 指定自定义端口启动混合服务
./toutiaohao-server -port 9000

# 启动交互式扫码登录以持久化保存 Cookie 凭证并退出
./toutiaohao-server -login

# 执行指定集成测试（封面上传、定时发布、存草稿逻辑验证）
cd toutiaohao && go test -v -run TestPublishArticleManual

# 执行正文插图上传专项集成测试
cd toutiaohao && go test -v -run TestPublishArticleBodyImageManual
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

6. **修改文章封面保留规则**：
   - 在修改文章（`UpdateArticle`）时，若用户没有显式传递封面相关的参数选项，必须跳过封面重新决策和自适应流程，直接保留原文章的原有封面不变，以规避网络图源重传引发的页面超时，并保护用户的创作原貌。

7. **DOM 动态交互与刷新同步**：
   - 对于依赖悬停（Hover）触发显示操作菜单（如“更多”及隐藏的“删除”按钮）的卡片布局，必须首先物理 `Hover()` 对应的卡片区域并稍作等待，再通过 XPath（在 Go-rod 中必须使用 `ElementX()` 而非 `Element()`）精确定位子元素。
   - **草稿删除批量勾选规则**：头条草稿箱列表和草稿编辑页可能都不提供单篇“删除”入口。删除草稿时必须优先进入 `/profile_v4/graphic/articles?status=draft`，物理点击左侧“草稿箱”，根据标题/ID 定位目标草稿行，勾选该行真实可见的复选框，再点击页面批量操作栏中的“删除”并处理确认弹窗。
   - **草稿箱导航时序规则**：若当前 URL 或目标 URL 已包含 `status=draft` / `status=1`，说明 SPA 已处于草稿列表路由，此时严禁再次点击左侧“草稿箱”导航；应等待列表卡片渲染并直接定位复选框。重复点击可能触发前端路由重入，导致文章卡片区空白或丢失。
   - **草稿删除元素打标规则**：定位草稿行复选框、批量“删除”按钮或确认按钮时，严禁对外层容器（如 `.pgc-content`、整张卡片、`[role=button]` 父容器）打标后点击。必须只给真实可见的小按钮本体打标，随后调用 `scrollIntoView({ block: 'center', inline: 'center' })` 并用 go-rod `Interactable()` 坐标物理点击；只有物理点击失败时才允许回退 JS 事件链。否则会出现 `element has no visible shape or outside the viewport`，操作不会触发。
   - 在触发涉及 DOM 删除等可能引起局部异步更新的操作后，必须通过 `page.Reload()` 重启导航加载，以此物理抹除已被后台删除的节点，防止旧 DOM 滞留导致的二次误判。
   - **消除顶部悬浮导航栏遮挡防线**：页面上下滚动时，顶部导航栏（如 `.shead_wrap` 等）极易遮挡正文图片上传按钮或封面槽，引发 go-rod 获取物理坐标失败（`Interactable` 报错）。在涉及物理定位与点击前，必须调用 `dismissObstacles()` 将 `.shead_wrap` 与 `[class*="shead_wrap"]` 等顶栏强行隐藏（设为 `display: none`）以保证稳定。

8. **React 受控状态固化与时序防线**：
   - **时序稳定期**：修改文章加载时由于存在异步数据填充，极易在随后的重绘中冲掉早期写入的数据。在检测到编辑器原有数据加载完毕后，必须多等待 **3秒左右的平息期**，平息一切首屏异步回调渲染，之后再触发新数据的录入。
   - **React 状态深层触达**：严禁仅设置 DOM 元素的 `value`。为防止 React 受控组件（Controlled Component）在表单提交时恢复原状态或提交旧 State，设置 value 后必须**主动遍历并触发 DOM 元素上由 React 绑定的底层事件监听器（调用 ho.onChange 与 ho.onInput 等合成事件回调）**以固化 React State。
   - **最终发布前一致性二次校验**：在最终发出 `clickPublish` 物理发布动作之前，必须进行标题与正文的**最终一致性二次固化校验**。若发现录入值因重绘发生偏移或被冲掉，需通过 React Value Tracker 劫持技术（遍历 `__reactProps$` / `__reactEventHandlers$` 键并主动调用 `ho.onChange` / `ho.onInput`）重新强行注入后再触发最终点击发布。
   - **修改文章"发布"按钮二次确认弹窗**：修改已发布文章后点击"发布"按钮时，头条平台通常会弹出二次确认弹窗（如"确认发布"对话框）。`clickPublish` 在检测到按钮文本为"发布"时，**绝不可直接跳过二次确认检测**，而必须短暂轮询等待（约 3 秒）检测是否有 `.semi-modal` / `.byte-modal` 弹窗弹出，若有则自动点击"确认发布"或"确定"按钮以完成最终提交。

9. **时间配置与抽屉防反折叠**：
   - 在勾选“定时发布”单选前，必须使用 `Eval` 智能检测“发文设置”抽屉当前是否已处于展开状态。只有在其折叠时才点击它进行展开，避免重复点击将本已展开的区域反向收起导致找不到日期输入控件。

10. **定时发布降级保障**：
    - 若定时发布日期/时间写入失效，只在控制台记录 `Warn` 警告，自动进行“优雅降级”，继续发文流程，避免由于时间格式、浏览器偶发渲染延迟而中断用户整篇文章的发文事务。

11. **正文图片占位符自动解析**：
    - 针对包含 `![描述](IMG_PLACEHOLDER_X)` 类似占位符格式的 Markdown 正文，系统应在录入编辑器前调用 `resolveImagePath`，将 `IMG_PLACEHOLDER_X` 动态翻译为 `opts.Images[X-1]` 的物理绝对路径，保障图文混排的顺畅上传。

12. **前置秒级自检与 400 状态码映射**：
    - 在运行耗时耗资源的无头浏览器逻辑前，增加秒级快速 HTTP 登录态自检。
    - 凡是业务、校验及防重引起的拦截（例如标题已存在、Cookie 过期已失效、参数为空等），一律在 API 控制器层映射为 `400 Bad Request` 并抛出可读中文字样，防止误报 `500` 系统内部错误混淆视听。

13. **工作区临时中转防权限沙盒（硬性发文规则）**：
    - 所有下载与被图片净化功能重构的临时图片文件，**必须**生成在当前项目工作区根目录下的隐藏目录 `./.tmp` 内，严禁使用系统的 `/tmp` 或 `/var/folders` 默认临时目录。
    - 这可防止由于无头/隔离环境下 Chrome 浏览器因 macOS 沙盒隔离权限限制无法跨卷/跨目录访问 `/var` 导致上传 0 字节损坏空文件（触发头条后台“无效图片数据”拦截）的缺陷。

14. **图片上传必须走 Chrome 文件选择器（硬性发文规则）**：
    - 正文插图与封面图上传严禁再直接对隐藏 `input[type=file]` 调用 `SetFiles`。头条图片上传组件会生成本地 blob 预览，但服务端可能只收到约 210 字节的空/损坏 body，并报“无效图片数据”。
    - 必须使用 go-rod `page.HandleFileDialog()` 先拦截 Chrome 原生文件选择器，再物理点击当前可见上传面板里的“本地上传”按钮，最后由文件选择器回调传入 Go 层动态转换后的绝对路径。
    - 上传触发器必须限定在当前可见的 `.upload-image-panel` / `.byte-modal` / `.semi-modal` / `[role="dialog"]` 等上传弹窗中，严禁抓取页面底层或历史遗留的全局 file input。
    - `/spice/image`、`upload_source=` 以及 `multipart/form-data` 上传请求必须绕过 `HijackRequests` 的 `LoadResponse` 代理，只能 `ContinueRequest` 原样放行；否则 multipart 文件体会被破坏并触发约 210 字节的“无效图片数据”请求。
    - 图片上传确认弹窗若未正常关闭，不允许“优雅降级继续发文”；必须保存错误截图并返回明确错误，防止正文或封面实际缺图却继续提交。

---

## 5. 本地开发与测试流程

AI 助手在修改项目后需进行完整的自我闭环验证，流程如下：

1. **静态代码编译检查**：
   在项目根目录下执行编译，确认无任何语法错误：
   ```bash
   go build ./...
   ```

2. **发文自动化流验证**：
   在大包 `toutiaohao/` 目录下执行集成测试，并确保当前终端可以拉起 Chrome 浏览器：
   ```bash
   go test -v -run TestPublishArticleManual
   ```
   **预期验证结果**：
   - 测试能够在控制台输出 `Loaded cookies from ../cookies.json`。
   - 自动生成 `testdata/test_cloud_computing.png` 临时图片。
   - 浏览器自动运行输入标题、自动上传测试图片、模拟排版、自动勾选虚构演绎声明并保存草稿。
   - 测试结束后自动输出 `PASS`，且 `testdata/` 目录被物理清除干净。

3. **正文插图上传专项验证**：
   在大包 `toutiaohao/` 目录下执行正文插图上传闭环测试：
   ```bash
   go test -v -run TestPublishArticleBodyImageManual
   ```
   **预期验证结果**：
   - 控制台输出 `Chrome 文件选择器已接收文件`。
   - 正文图片确认弹窗成功关闭，不出现“无效图片数据”。
   - 测试结束后输出 `PASS`，临时草稿会在 `defer` 中尝试清理。

4. **修改文章自动化流验证 (安全沙盒)**：
   在 `toutiaohao/` 目录下执行修改文章全链路闭环测试：
   ```bash
   go test -v -run TestUpdateArticleManual
   ```
   **预期验证结果**：
   - 流程能够自动发布带有时间戳的临时新文章，从列表中提取 ID 自动导航到 `/graphic/publish?pgc_id=...`。
   - 正确执行物理 Hover 清空及标题覆盖键入，并在 `opts` 为空时，打印 `修改文章时未显式指定封面，将保持原有封面不变`。
   - 二次发布确认能够在 Modal 对话框中正确点击，发布修改成功并最终在 `defer` 中将临时测试文章完全物理删除。
   - 测试结束后输出 `PASS`。
