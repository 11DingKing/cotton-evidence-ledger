# BENZHI_README

这是一个 Go 后端服务，用于棉花科研机构协作登记、审校、发布、纠错和追溯论文、专利、标准、著作与品种证据。

## 环境要求

- Go 1.24.0 或兼容的更新工具链
- Docker 24 或更高版本（用于容器构建）
- SQLite 由 Go 驱动内嵌，无需单独安装服务

## 标准构建、运行和测试命令

进入容器后执行：

```bash
# 编译
cd '/app' && GOTOOLCHAIN=local go build ./...

# 启动
cd '/app' && GOTOOLCHAIN=local go run ./cmd/server

# 测试
cd '/app' && GOTOOLCHAIN=local go test ./...
```

服务默认监听 `:8080`，数据库默认写入 `cotton-evidence.db`。可使用 `.env.example` 中的 `COTTON_*` 环境变量调整监听地址、数据库路径、会话时长、worker 租约及初始知识负责人账号。

## Docker 构建和进入容器

```bash
chmod +x build_benzhi_docker.sh
./build_benzhi_docker.sh cotton-evidence-benzhi-amd64 linux/amd64
./build_benzhi_docker.sh cotton-evidence-benzhi-arm64 linux/arm64
docker run -it cotton-evidence-benzhi-amd64:latest
docker run -it --platform linux/arm64 cotton-evidence-benzhi-arm64:latest
```
