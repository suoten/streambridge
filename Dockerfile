# 多阶段构建: 编译阶段 + 运行阶段
FROM golang:1.22-alpine AS builder

WORKDIR /src

# 缓存依赖
COPY go.mod go.sum ./
RUN go mod download

# 拷贝源码并编译(静态链接)
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags "-s -w -X main.version=docker" \
    -o /out/streambridge ./cmd/streambridge

# 运行阶段: 最小镜像
FROM alpine:3.24

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
