# 古生物修复制备台

古生物修复制备台是一个使用 Go 编写的后端基线，用于记录化石标本、审批修复方案、回填制备进度，并在全部工序完成后办理研究借阅放行。

该服务只使用进程内仓储，便于在本机重复启动和检查。它不依赖数据库、远端接口或其他在线资源。

## 目录

- `cmd/paleo-prep-station`：HTTP 服务入口和监听参数。
- `cmd/workflowcheck`：通过真实 HTTP 处理器执行四条业务链路的本地验证命令。
- `internal/httpapi`：路由、请求解码、状态码映射和 JSON 输出。
- `internal/station`：跨领域对象的编排、事务边界和业务流程。
- `internal/domain`：标本、方案、进度和放行记录的状态与不变量。
- `internal/repository`：互斥保护的内存仓储和原子提交。
- `internal/contracts`：稳定错误模型和公共请求结构。

## 构建与运行

```bash
go build ./...
go run ./cmd/paleo-prep-station --addr 127.0.0.1:18133
```

服务启动后，`GET /healthz` 返回就绪状态。

## 主要接口

- `GET /specimens` 与 `POST /specimens`
- `GET /specimens/{id}`
- `POST /specimens/{id}/plans`
- `GET /plans/{id}`
- `POST /plans/{id}/approval`
- `POST /plans/{id}/progress`
- `GET /plans/{id}/materials`
- `POST /plans/{id}/material-checkouts`
- `POST /plans/{id}/material-consumptions`
- `POST /plans/{id}/material-returns`
- `GET/POST /material-batches` 与 `GET /material-batches/{id}`
- `POST /specimens/{id}/release`

## 耗材批次与方案用量

胶结剂、加固液和包埋材料按批次入库（`POST /material-batches`），每个批次持有唯一的货架余额，并随入库流水建立台账。方案只能引用未过期且余额充足的批次：

- 领用（checkout）在同一事务内扣减批次余额并建立方案用量行；同一方案对同一批次存在未结清领用时，重复领用直接 `409`。
- 消耗（consumption）只能发生在该方案已领用且尚未结清的余量内，不再次扣减货架余额；过期批次即使已领用也禁止消耗。
- 退料（return）只能退回实际持有的未用量，退回后货架余额恢复，且不得超过入库数量。
- 方案进度达到 100% 时，所有领用行必须已结清（消耗或退料完毕），否则方案完成失败且不留下半成品状态。

`GET /plans/{id}` 返回方案的每条用量行（领用/消耗/退料累计），`GET /material-batches/{id}` 返回批次当前余额与全部流水，二者事实始终一致。

## 本地验证

每条业务链路都可以独立执行：

```bash
go run ./cmd/workflowcheck --workflow register-specimen
go run ./cmd/workflowcheck --workflow approve-prep-plan
go run ./cmd/workflowcheck --workflow log-preparation
go run ./cmd/workflowcheck --workflow release-for-study
go run ./cmd/workflowcheck --workflow track-material-batches
```

这些检查会启动临时 HTTP 处理器并调用真实公共路由。

## 环境变量

项目不需要环境变量，也没有外部服务依赖。

## 测试边界

本基线按要求暂不生成单元测试、测试夹具或端到端测试文件。后续任务阶段会补充红绿验证测试，届时再替换或扩展当前的生产级链路检查。
