# 1. 编译阶段：直接复用本地现有的 golang:1.26-bookworm 镜像
FROM golang:1.26-bookworm AS builder

WORKDIR /src

# 设置国内 Go 模块代理，加速下载依赖
ENV GOPROXY=https://goproxy.cn,direct

COPY go.mod ./
RUN go mod tidy

COPY . .

# 静态编译输出无依赖的单一执行文件
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/wechat-server ./cmd/server

# 2. 运行阶段：使用已有镜像，无需从外网下载新底包
FROM golang:1.26-bookworm

WORKDIR /app
COPY --from=builder /bin/wechat-server /app/wechat-server

EXPOSE 5000

ENTRYPOINT ["/app/wechat-server"]
