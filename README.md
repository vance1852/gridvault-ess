# GridVault ESS

GridVault ESS 是一个生产级储能调度后端，管理站点与电池簇、容量预留、四眼审批、持久化执行任务、遥测告警、结算周期和审计事件。服务以 SQLite 作为真实关系数据库，通过版本化 migration、外键、唯一约束、条件更新和事务维持跨实体不变量。

## 核心流程

- 调度员创建计划并选择设备簇，提交时原子预留站点功率和设备时间窗；操作员审批并下发后，带 lease 的 worker 执行持久化任务。
- 遥测上报以设备序列号幂等推进，SOC 或温度越界时创建唯一活动告警；告警确认和恢复处置均记录操作者与请求 ID。
- 结算周期冻结已完成计划，按实际遥测电量生成唯一条目并保留偏差和审计。
- 登录签发仅保存哈希的随机 session token；退出立即撤销，鉴权时检查撤销和过期。

## 本地验证和运行

```bash
go mod download
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./...
```

设置 `GRIDVAULT_BOOTSTRAP_EMAIL` 与满足复杂度要求的 `GRIDVAULT_BOOTSTRAP_PASSWORD` 后执行 `go run ./cmd/server`。存活与就绪端点分别为 `/healthz`、`/readyz`。

## 数据库与 Docker

首次启动自动执行 `migrations` 中的有序 SQL；重复启动只读取已应用版本，历史冲突会阻止启动。根 `Dockerfile` 从真实 `./cmd/server` 构建并默认启动 `/app/gridvault`：

```bash
docker build --platform linux/amd64 -t gridvault-ess:amd64 .
docker run --rm -p 8080:8080 gridvault-ess:amd64
```
