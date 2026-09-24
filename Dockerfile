# 多阶段构建: 编译阶段 + 运行阶段
FROM golang:1.23-alpine AS builder

# git 需要 for go mod download(某些依赖需要从 git 获取)
RUN apk add --no-cache git

WORKDIR /src

# 缓存依赖
COPY go.mod go.sum ./
RUN go mod download

# 拷贝源码并编译(静态链接)
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -mod=mod -trimpath -ldflags "-s -w -X main.version=docker" \
    -o /out/streambridge ./cmd/streambridge

# 运行阶段: 最小镜像
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 1000 streambridge

WORKDIR /app
COPY --from=builder /out/streambridge /usr/local/bin/streambridge
COPY configs/streambridge.yaml /etc/streambridge/config.yaml

USER streambridge
EXPOSE 8080 1935 1985 20000-30000/udp

HEALTHCHECK --interval=30s --timeout=5s --retries=3 --start-period=10s \
    CMD wget -qO- http://localhost:8080/api/health || exit 1

ENTRYPOINT ["streambridge"]
CMD ["-c", "/etc/streambridge/config.yaml"]
