# Cotton Evidence Ledger

Cotton Evidence Ledger 是面向棉花科研机构的证据协作后端。资料采集员、领域研究员、独立审校专家和知识负责人可以把论文、专利、标准、著作与品种资料整理为带版本、论断、审校意见和引用关系的证据单元，并完整保留发布、纠错、替代、撤回、恢复及责任交接历史。

## 主要能力

- 来源登记、指纹查重、论断提取和版本化证据管理。
- 提交者隔离的交叉审校，以及数据库唯一约束保护的审校名额。
- 发布状态、版本状态、引用链和审计事件的一致事务。
- 纠错版本替代、入站引用重连、撤回占用检查和责任交接。
- 可撤销服务端会话、过期处理和四类业务角色权限。
- SQLite WAL、版本化 migration、乐观版本和幂等数据结构。
- 持久化 worker 租约、重启恢复、退避重试和永久失败。
- append-only 审计哈希链、请求 ID、JSON 错误、存活与就绪检查。

## 本地运行

```bash
cp .env.example .env
go mod download
go run ./cmd/server
```

服务默认监听 `http://127.0.0.1:8080`。首次启动会创建知识负责人账号，开发默认值为 `owner@example.test` / `change-this-password`，部署时必须通过环境变量替换。

```bash
curl -fsS http://127.0.0.1:8080/health/live
curl -fsS http://127.0.0.1:8080/health/ready
```

## 验证

```bash
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./...
```

## Docker

项目 `Dockerfile` 构建只包含运行二进制的生产镜像，默认入口直接启动 API 服务。`benzhi.Dockerfile` 保留完整 Go 工具链，供隔离环境内编译和测试。

```bash
docker build --platform linux/amd64 -t cotton-evidence-ledger:amd64 .
docker run --rm -p 8080:8080 -e COTTON_BOOTSTRAP_PASSWORD='replace-with-a-secure-password' cotton-evidence-ledger:amd64
```

## 数据结构

migration 从空库建立 users、sessions、sources、evidence_units、evidence_versions、claims、review_slots、reviews、citations、jobs、idempotency_keys、audit_events、responsibility_handoffs 和 notifications。所有时间以 UTC RFC3339Nano 保存；业务截止时间由 API 传入并在服务层统一换算为 UTC。
