# 微信公众号多平台验证码中枢系统 (WeChat MP Hub)

基于 **Go (Golang) + Redis** 打造的高性能、极致轻量、7x24 小时稳定的微信公众号消息与验证码路由转发中枢。

---

## 目录结构

本项目按标准 Go 工业分层结构组织，各模块职责独立：

```text
wechat-mp/
├── cmd/
│   └── server/
│       └── main.go          # 程序入口，负责初始化配置、Redis连接、注册路由和优雅关机
├── internal/
│   ├── config/
│   │   └── config.go        # 配置中心：自动读取环境变量与 .env
│   ├── model/
│   │   ├── wechat.go        # 微信 XML 协议报文结构与 CDATA 封装
│   │   └── verify.go        # 验证码数据结构、API 请求与响应定义
│   ├── storage/
│   │   └── redis.go         # Redis 存储层：原子存取验证码、核销、设置 TTL 自动失效
│   ├── service/
│   │   ├── wechat_service.go# 微信业务层：SHA-1 签名校验、用户消息/事件解析与回复组装
│   │   └── verify_service.go# 验证码业务层：防碰撞 6 位随机码生成与状态核验
│   └── handler/
│       ├── wechat_handler.go# HTTP 控制器：/wechat 路由（GET握手、POST消息推送）
│       ├── verify_handler.go# HTTP 控制器：/api/code/create, /api/code/check, /health
│       └── helper.go        # 通用 JSON 响应工具函数
├── Dockerfile               # 容器化构建文件（复用本地已有 golang:1.26-bookworm 镜像）
├── docker-compose.yml       # Docker 编排文件（包含 Go 应用与 Redis 8-alpine）
├── .env                     # 生产/本地配置文件（包含 Token、广告语等）
├── .env.example             # 配置模板
├── go.mod                   # Go 依赖清单
└── go.sum                   # Go 依赖锁定校验文件
```

---

## 快速启动（100% 容器化运行）

无需在宿主机配置任何 Go 或 Redis 环境，直接在当前目录使用 Docker 启动：

### 1. 启动服务与 Redis
```bash
docker compose up -d --build
```

### 2. 本地可视化高仿调试沙箱（强烈推荐）
服务启动后，直接在浏览器中打开：
👉 **http://localhost:5000/debug**（或直接访问 `http://localhost:5000`）

你将获得：
* **📱 1:1 像素级复刻微信公众号聊天界面**：包含导航栏、绿色微信气泡、白色公众号回复、链接可点击。
* **📋 微信底部自定义菜单原生交互**：
  * 支持在“键盘打字”与“菜单按钮”之间自由切换；
  * 点击一级菜单向上弹出二级子菜单；
  * 点击菜单项自动向后端推送 `CLICK` 事件。
* **🛠️ 在线可视化菜单编辑器**：实时添加/编辑一级菜单与二级菜单，点击保存立即同步到模拟器。
* **💻 业务项目端联动测试**：模拟项目 A 生成 6 位验证码，一键在微信发送，实时展示核销成功状态！

### 3. 查看运行日志
```bash
docker compose logs -f app
```
看到类似如下输出，即表示服务与 Redis 已成功连接：
```text
>>> 正在启动微信公众号多平台验证码中枢系统...
[Config] 服务端口: 5000 | 微信Token: mywechat2025 | Redis地址: redis:6379
[Redis] 连接成功！
>>> 🚀 服务已成功监听端口 :5000
```

### 3. 停止服务
```bash
docker compose down
```

---

## 微信公众号对接配置指南

### 第一步：打通公网访问（本地测试环境）
因为你的代码运行在本地电脑的 Docker 中，微信服务器在公网，需要一个公网可访问的通道（支持 80/443 端口）：
* 推荐使用 **cpolar**（或 Sunny-Ngrok）：
  ```bash
  cpolar http 5000
  ```
* 运行后会获得一个公网 HTTPS 地址，例如：`https://xxxx.cpolar.top`。

### 第二步：前往微信后台填写配置
在微信开发者平台「基础信息」->「域名与消息推送配置」->「消息推送」点击【启用】：
* **URL**：`https://xxxx.cpolar.top/wechat`
* **Token**：与 `.env` 中的 `WECHAT_TOKEN` 一致（默认 `mywechat2025`）
* **EncodingAESKey**：点击右侧【随机生成】
* **消息加密**：选择 **【明文模式】**
* **数据格式**：选择 **【XML】**
* 点击【确定】，微信会向你发送测试请求，后端控制台将显示 `[WeChat GET] 微信接入验证成功！`，配置即刻生效。

---

## 业务项目接入 API 指南

你的各种业务项目（项目 A、项目 B、项目 C...）通过标准 RESTful 接口与本系统交互：

### 1. 项目生成验证码（方案一：网站展示验证码）
* **请求方式**：`POST /api/code/create`
* **请求体**：
  ```json
  {
    "project": "blog",
    "extra": "user_session_10086",
    "expire_seconds": 300
  }
  ```
* **返回示例**：
  ```json
  {
    "code": 0,
    "message": "success",
    "data": {
      "code": "839201",
      "project": "blog",
      "expire_seconds": 300
    }
  }
  ```
* **业务前端操作**：网页弹窗显示：`“请向微信公众号发送验证码 839201”`。

---

### 2. 网页前端轮询核销状态
* **请求方式**：`GET /api/code/check?code=839201`
* **未核销（用户还没发）**：
  ```json
  {
    "code": 0,
    "message": "success",
    "data": {
      "code": "839201",
      "project": "blog",
      "status": "pending",
      "verified": false
    }
  }
  ```
* **已核销（用户已在微信发送）**：
  ```json
  {
    "code": 0,
    "message": "success",
    "data": {
      "code": "839201",
      "project": "blog",
      "status": "verified",
      "verified": true,
      "openid": "oxxxxxxxxxxxxxxxx",
      "verified_at": 1711234567,
      "extra": "user_session_10086"
    }
  }
  ```
* **业务前端操作**：检测到 `verified: true`，立即为用户放行或登录！

---

## 广告与文案自定义

在 `.env` 文件中修改 `AD_CONTENT` 参数，即可全局改变公众号所有回复消息末尾的宣传语或推广网址，无需重新编译代码，重启容器即生效。
