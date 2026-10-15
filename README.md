# zal-blog

个人博客（界面英文、内容中文）：终端复古 TUI 风格，文章以 org-mode 格式撰写。
Go 程序负责把 org 渲染成静态 HTML/JSON，产物可直接托管到 GitHub Pages。

## 两种运行方式

```sh
cd backend

# 1. 开发模式：本地动态服务器（改文件刷新即生效）
go run . serve
# 打开 http://localhost:54345

# 2. 静态生成：把 org 渲染成静态站点写入 frontend/（默认动作）
go run .                  # 等价于 go run . generate
go run . generate https://example.com   # 自定义站点地址（影响 rss.xml 里的链接）
```

`SITE_BASE` 环境变量也可设置站点地址。端口默认 `54345`，`PORT` 可覆盖；`BLOG_DIR` / `FRONTEND_DIR` 可覆盖内容与前端目录（默认 `blog` / `frontend`）。

## 静态产物结构（generate 后）

```
frontend/
├── index.html / blog.html / tags.html / about.html / 404.html
├── posts.json                文章列表（前端 JS 读取）
├── tags.json                 标签统计
├── rss.xml                   RSS 订阅
├── assets/                   从 blog/assets/ 复制
├── posts/YYYY/MM/slug.html   每篇文章一个完整 HTML 页
└── series/SLUG.{html,json}   每个系列一个落地页 + 数据
```

整个 `frontend/` 就是一个可独立托管的静态站点（GitHub Pages 等）。

## 写文章

- 文章放在 `blog/YYYY/MM/slug.org`（按年月分目录），例如 `blog/2025/08/hello-zal-blog.org`
- 头部元信息（org 关键字）：
  - `#+TITLE: 标题`（必填，缺省取第一个 `*` 标题）
  - `#+DATE: 2025-08-25`（缺省取路径中的年月）
  - `#+TAGS: :tag1:tag2:`（可省略）
  - 草稿：文件名以 `.sec.org` 结尾（如 `todo.sec.org`）。不出现在列表/标签/RSS/静态站中，且被 .gitignore 忽略不进仓库；开发模式下可直接访问预览（URL 用 `2026/08/todo.sec`）
- 文章按日期倒序排列

### 系列文章

系列内容放在 `blog/series/SLUG/`（SLUG 为英文目录名，进入 URL）：

```
blog/series/sicp-notes/
├── index.org                系列元信息 + 简介
├── 01-chapter-one.org       章节 01
└── 02-chapter-two.org       章节 02
```

- `index.org`：
  - `#+TITLE:` 系列标题（展示在博客列表里，形如 `Title[Count]`）
  - `#+TAGS:` 系列标签
  - 正文作为系列简介，渲染在系列落地页
  - 无需写 `#+DATE` 和文章数量：开始/最后日期自动取章节 `#+DATE` 的最早/最晚值，`[Count]` 自动统计
  - 可选 `#+SUFFIX_ORDER: TRUE`：声明本系列使用**后置**数字排序（见下）
- 章节序号（默认**前导**）：`NN-name.org`，`NN` 任意位数、只决定系列内顺序；
  如 `01-chapter-one.org` → `/posts/series/sicp-notes/01-chapter-one.html`。
  章节可选写 `#+DATE` / `#+TITLE`；不写 `#+DATE` 则不参与系列日期统计。
  草稿同样用 `.sec.org` 后缀（如 `03-draft.sec.org`）。
- 章节序号（**后置**）：当 `index.org` 里写了 `#+SUFFIX_ORDER: TRUE`，
  则按文件名**末尾**数字排序，适合比赛题单这类天然带编号的系列：
  `Atcoder-Beginner-Contest-475.org` → 序号 475（URL 保留完整文件名）。
- 博客列表里整个系列折叠为一条 `Title[Count]`，按系列最后日期排序；
  点进去是 `/series/SLUG.html`（简介 + 开始/最后日期 + 标签 + 章节列表）。
- 系列图片放 `blog/assets/series/SLUG/`。

## 图片

图片放在 `blog/assets/` 下，目录层级与文章镜像对应：
文章 `blog/2025/08/x.org` 的图片放 `blog/assets/2025/08/pic.png`，
文中引用 `[[file:pic.png]]` 或 `[[file:pic.png][alt 文字]]`。
生成时自动复制到 `frontend/assets/`。

## 支持的 org 语法

标题 `*`~`*****`（映射为 h2~h6）、段落、粗体/斜体/下划线/删除线、
行内代码 `~x~` / `=x=`、代码块 `#+begin_src lang`、引用块 `#+begin_quote`、
链接 `[[url][text]]` 与裸 URL、图片 `[[file:...]]`、无序/有序/嵌套列表、
表格、分隔线 `-----`。

## 路由

| 路径 | 说明 |
|------|------|
| `/` | 首页 |
| `/blog.html` | 博客列表（读 `posts.json`） |
| `/posts/2025/08/slug.html` | 文章详情（静态生成；系列章节为 `/posts/series/SLUG/NN-name.html`） |
| `/series/SLUG.html` | 系列落地页（读 `series/SLUG.json`：简介、日期范围、标签、章节列表） |
| `/tags.html`（`?t=tag` 过滤） | 标签（读 `tags.json` + `posts.json`） |
| `/about.html` | 关于（构建期预渲染，正文来自 `blog/about.org`） |
| `/rss.xml` | RSS 订阅 |
| `/assets/...` | 图片资源 |

开发模式服务器额外提供 `/api/posts`、`/api/post?p=...`、`/api/tags`、`/api/about` 调试用。

## 部署

`frontend/` 为纯静态站点：

- **GitHub Pages**：推送 `frontend/` 内容到 Pages 仓库（本仓库模块名即
  `zelo-ex.github.io`，可直接作为用户 Pages 仓库使用）
- **自建服务器**（如腾讯云）：把 `frontend/` 交给任意静态文件服务器（nginx 等）

### GitHub Actions 自动部署

`.github/workflows/deploy.yml`：push 到 main 时自动跑测试、生成静态站点并部署到
GitHub Pages；PR 只跑测试。首次使用需要在仓库 Settings → Pages 里把
Source 设为 **GitHub Actions**。

注意：站点内链接使用根相对路径（如 `/posts/...`），适用于部署在域名根部的
用户 Pages 仓库（zelo-ex.github.io）。若改用项目仓库（子路径如
`/zal-blog/`），需要生成器支持 basepath（未实现）。
