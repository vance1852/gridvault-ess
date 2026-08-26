# BENZHI_README

这是一个面向多站点储能调度的 Go 后端服务，负责容量预留、计划执行、遥测告警、结算审计与可撤销会话管理。

## 环境要求

- Go 1.22.5
- module：`github.com/vance1852/gridvault-ess`
- Docker 与 BuildKit

## 标准构建、运行和测试命令

进入容器后执行：

```bash
# 编译
cd /app && GOTOOLCHAIN=local go build ./...
# 启动
cd /app && GOTOOLCHAIN=local go run ./cmd/server
# 测试
cd /app && GOTOOLCHAIN=local go test ./... -count=1
cd /app && GOTOOLCHAIN=local go test -race ./... -count=1
```

服务默认监听 `:8080`，数据库默认保存到 `data/gridvault.db`，环境变量可覆盖监听地址、数据库路径、会话时限与 worker 周期。

## Docker 构建和进入容器

```bash
chmod +x build_benzhi_docker.sh
./build_benzhi_docker.sh gridvault-ess-benzhi-amd64 linux/amd64
./build_benzhi_docker.sh gridvault-ess-benzhi-arm64 linux/arm64
docker run -it gridvault-ess-benzhi-amd64:latest
docker run -it --platform linux/arm64 gridvault-ess-benzhi-arm64:latest
```
