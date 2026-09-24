# StreamBridge Makefile
# 单二进制构建,支持交叉编译与静态资源嵌入
# 需要 Go 1.23+

VERSION ?= v1.0.0
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

GOFLAGS := -trimpath -ldflags "$(LDFLAGS)"

# 默认目标:构建当前平台
.PHONY: build
build:
	go build $(GOFLAGS) -o bin/streambridge ./cmd/streambridge

# Windows
.PHONY: build-windows
build-windows:
	GOOS=windows GOARCH=amd64 go build $(GOFLAGS) -o bin/streambridge-windows-amd64.exe ./cmd/streambridge

# Linux
.PHONY: build-linux
build-linux:
	GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -o bin/streambridge-linux-amd64 ./cmd/streambridge

.PHONY: build-linux-arm64
build-linux-arm64:
	GOOS=linux GOARCH=arm64 go build $(GOFLAGS) -o bin/streambridge-linux-arm64 ./cmd/streambridge

# macOS
.PHONY: build-darwin
build-darwin:
	GOOS=darwin GOARCH=arm64 go build $(GOFLAGS) -o bin/streambridge-darwin-arm64 ./cmd/streambridge

# 全平台交叉编译
.PHONY: build-all
build-all: build-linux build-linux-arm64 build-windows build-darwin

# Docker 镜像
.PHONY: docker
docker:
	docker build -t streambridge:$(VERSION) -t streambridge:latest .

# 运行测试
.PHONY: test
test:
	go test -v ./...

# 运行
.PHONY: run
run:
	go run ./cmd/streambridge -c configs/streambridge.yaml

# 清理
.PHONY: clean
clean:
	rm -rf bin/

# 代码格式化
.PHONY: fmt
fmt:
	gofmt -s -w .

# 静态检查
.PHONY: lint
lint:
	go vet ./...

.PHONY: tidy
tidy:
	go mod tidy
