# 微信公众号多平台验证码中枢系统 (WeChat MP Hub)

基于 **Go (Golang) + Redis** 打造的高性能、轻量、高安全、7x24 小时稳定的微信公众号消息与验证码路由转发中枢。

---

## 核心特性

* **双流向验证架构**：
  * **流向一（微信索码填入）**：适合 APP/H5/表单。用户在公众号发送【验证码】获取 6 位通行码，填入网站表单，网站后端向中枢发起单次核销（替代高昂短信费，单次消费防重放）；
  * **流向二（网站弹码免填）**：适合电脑与移动端浏览器。网页申请 6 位码，用户在公众号回复该数字，前端轻量轮询自动放行。
* **严格通信密钥（Key）白名单防御**：各业务系统接入必须携带专属 `key`，非白名单或无效密钥一律以 `401 Unauthorized` 拦截，杜绝接口滥用。
* **清晰机器与展示契约**：接口始终返回规范的英文代号 `project`（供机器比对）与友好中文名 `project_name`（供展示），并支持大小写容错。
* **四种规范尾注展示模式**：
  * 叠加模式：专属尾注 + 系统默认尾注
  * 专属模式：专属尾注
  * 默认模式：系统默认尾注
  * 纯净模式：无任何尾注
* **数据与配置彻底隔离**：
  * 具备 `config.default/` 出厂模版，冷启动零配置秒级自愈；
  * 运行配置（`config/*.json`）由 `.gitignore` 保护，本地测试脏数据绝不污染代码仓库；
  * Docker 数据卷（Volume）挂载，服务重新部署、版本升级，历史业务项目与配置 100% 完好无损。
* **Redis AOF 实时持久化**：开启 Append-Only File 落盘机制，容器或服务器意外重启，活跃验证码不丢失。

---

## 目录结构

```text
wechat-mp/
├── cmd/
│   └── server/
│       └── main.go          # 程序入口：路由分发、优雅关机、根路径跳转 /console
├── config.default/          # 纯净出厂配置模版（新环境拉取自动自愈）
│   ├── auth.json            # 默认控制台密码 (admin)
│   ├── footers.json         # 默认系统尾注模版
│   ├── menu.json            # 默认微信菜单
│   ├── projects.json        # 默认示例接入项目 (shop, blog, nav)
│   └── replies.json         # 默认场景回复文案
├── internal/
│   ├── config/              # 配置中心
│   ├── model/               # 微信协议、业务实体与 API 请求响应模型
│   ├── storage/             # Redis 存储与 AOF 持久化操作层
│   ├── service/             # 微信回复逻辑、项目多租户隔离、验证码全生命周期服务
│   └── handler/             # HTTP 接口控制器 (微信路由、验证码 API、控制台 API)
├── web/
│   └── console.html         # 响应式管理控制台与 1:1 像素级微信手机仿真沙箱
├── Dockerfile               # 容器化镜像构建
├── docker-compose.yml       # 容器编排文件 (应用 + Redis 8-alpine 持久化)
├── .env                     # 环境变量配置
└── README.md
```

---

## 快速启动（100% 容器化运行）

无需在宿主机安装 Go 或 Redis，直接在当前目录使用 Docker 一键拉起：

### 1. 启动服务与 Redis
```bash
docker compose up -d --build
```

### 2. 访问可视化管理控制台
服务启动后，直接在浏览器中打开：
👉 **http://localhost:5000**（或访问 `http://localhost:5000/console`）

* **默认访问密码**：`admin`
* **功能板块**：
  * **仿真调试沙箱**：内置 1:1 微信公众号手机交互模拟器，双流向全流程可视化联调；
  * **业务项目管理**：接入新系统、分配独立 Key、在线开关与四种尾注模式切换；
  * **默认尾注模版库**：系统级通用推广语与服务公告维护；
  * **消息回复与文案**：在线自定义欢迎语、核销通过提示、索码模板；
  * **接入文档与指南**：标准 API 协议文档与 AI 对接 Prompt 提示词一键生成。

### 3. 查看运行日志
```bash
docker compose logs -f app
```

### 4. 停止服务
```bash
docker compose down
```

---

## 业务项目接入 API 指南

所有外部业务系统均通过标准的 RESTful JSON 接口与中枢通信：

> 📌 **编号变更说明（v2.0.0）**：流向编号已对调——原「流向二（微信索码填入）」现为**流向一**，原「流向一（网站弹码免填）」现为**流向二**。所有接口路径、请求参数与响应结构零变化，已对接的业务方无需修改任何代码，重读本教程即可。

### 1. 主动核销通用通行码（流向一）
* **场景**：用户在公众号回复【验证码】获取 6 位数字，填入网站表单，网站后端向中枢发起核销。
* **接口**：`POST /api/code/verify-universal`
* **Headers**：`Content-Type: application/json`
* **请求体**：
  ```json
  {
    "project": "shop",
    "key": "sec_shop_8f9a2b1c",
    "code": "209710"
  }
  ```
* **核销成功响应**：
  ```json
  {
    "code": 0,
    "message": "success",
    "data": {
      "valid": true,
      "openid": "oXYZ_xxxxxxxxxxxx",
      "project": "shop",
      "project_name": "商城系统",
      "verified_at": 1711234567
    }
  }
  ```
* **重复提交响应（防重放）**：
  ```json
  {
    "code": 40002,
    "message": "该验证码已于早前完成核销，请勿重复使用",
    "data": {
      "valid": false,
      "error_type": "code_already_used"
    }
  }
  ```
### 2. 申请 6 位验证码（流向二）

> 接入契约：请求必须携带 `session_tag`（后端签发的会话标识，同一访客/尝试保持稳定）或转发 `X-Forwarded-For` 用户真实 IP，二者缺一返回 403；轮询必须使用响应中的 `poll_token`，6 位码本身不可用于查询。

* **接口**：`POST /api/code/create`
* **Headers**：`Content-Type: application/json`
* **请求体**：
  ```json
  {
    "project": "shop",
    "key": "sec_shop_8f9a2b1c",
    "session_tag": "user_session_10086",
    "expire_seconds": 300
  }
  ```
* **成功响应**：
  ```json
  {
    "code": 0,
    "message": "success",
    "data": {
      "code": "839201",
      "poll_token": "9f2c7a1e8b3d4f6a9c0e1d2b3a4f5c6d",
      "project": "shop",
      "project_name": "商城系统",
      "expire_seconds": 300
    }
  }
  ```

---

### 3. 轮询核销状态（流向二）
* **接口**：`GET /api/code/check?poll_token=9f2c7a1e8b3d4f6a9c0e1d2b3a4f5c6d`
* **未核销响应**：
  ```json
  {
    "code": 0,
    "message": "success",
    "data": {
      "code": "839201",
      "project": "shop",
      "project_name": "商城系统",
      "status": "pending",
      "verified": false
    }
  }
  ```
* **已核销响应（用户已在微信发送该纯数字）**：
  ```json
  {
    "code": 0,
    "message": "success",
    "data": {
      "code": "839201",
      "project": "shop",
      "project_name": "商城系统",
      "status": "verified",
      "verified": true,
      "openid": "oXYZ_xxxxxxxxxxxx",
      "verified_at": 1711234567,
      "extra": "user_session_10086"
    }
  }
  ```
* **已失效/不存在响应**：
  ```json
  {
    "code": 404,
    "message": "验证码已失效或不存在",
    "data": {
      "status": "expired",
      "verified": false,
      "error_type": "code_not_found"
    }
  }
  ```

---

