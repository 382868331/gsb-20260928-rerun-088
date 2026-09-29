# 服务性能调查平台（Go + React）

导入人工调用记录，按请求还原一次调用的因果关系，并把每个调用的耗时拆成
「实际执行」与「等待子调用」，重点回答：**时间花在等待下游，还是花在自己身上**。

## 设计原则

- **并行不相加**：子调用耗时按时间区间**并集**合并。两个并行 6s 的子调用只覆盖 7s，等待就是 7s，而不是 12s；原始求和 `overlapNs` 仍保留，差额即可见的并行重叠量。
- **跨设备时钟偏差不强行对齐**：时间戳原样保存、原样展示。只有能确定落在父调用窗口内的子区间才计入等待；超出窗口（超过 1ms 容差，网络/排队抖动）的部分计入 `outsideNs`，并打 `clock_anomaly` 标记，而不是挪动时间硬凑。
- **缺失父调用不臆造节点**：引用了不存在父 id 的调用打 `missing_parent`，作为独立根组件展示，并在 trace notes 中记录。
- **重复记录不丢弃**：相同 id 的后续记录单独保留为证据（节点上可展开查看），打 `duplicate`，第一条记录参与建树。
- **保存后可复算**：调查只保存来源批次、trace/root 选择与人工结论；重新打开时从原始记录重新建树、重新拆分（`recomputed=true`），不依赖保存时的派生数字。
- **采集格式可扩展**：`backend/internal/ingest` 用解析器注册表，本轮提供 `manual-jsonl`，后续格式注册即可。

## 耗时拆分（每个节点）

- `executingNs` = 自身耗时 − 子调用区间并集（未被任何子调用覆盖的时间）
- `waitingNs` = 直接子调用裁剪到自身窗口后的区间并集长度
- `overlapNs` = 直接子调用裁剪后的区间长度原始求和；`overlapNs − waitingNs` 即并行重复覆盖量
- `outsideNs` = 子调用落在自身窗口之外、超过 1ms 容差的部分（跨设备时钟偏差证据）
- 零/负耗时节点标记 `clock_anomaly`，不输出可误导的拆分数字

## 目录

- `backend/` Go 服务：`internal/model`（领域模型）、`internal/ingest`（解析器）、`internal/analysis`（因果树+拆分）、`internal/store`（JSON 文件存储：批次原始证据 + 调查视图）、`internal/api`（REST）、`cmd/server`。
- `frontend/` React 19 + Vite：批次导入、慢请求列表（标记列）、调用树/瀑布（执行/等待双色条）、原始证据面板（含重复记录）、调查保存与重新打开复算。
- `backend/examples/manual-sample.jsonl` 含并行子调用、缺失父调用、重复记录的示例数据。

## 运行

```bash
# 后端测试
cd backend && go test ./...

# 启动 API（:8080），数据写入 ./data
cd backend && go run ./cmd/server -addr :8080 -data ./data

# 前端开发（自动代理 /api -> :8080）
cd frontend && npm install && npm run dev

# 或构建后由 Go 直接托管
cd frontend && npm run build
cd backend && go run ./cmd/server -static ../frontend/dist
```

打开页面 → 在左侧粘贴 `backend/examples/manual-sample.jsonl` 内容（或点「载入示例」）→ 导入批次 → 在「慢请求」表中点选请求 →
查看瀑布与耗时卡片，点任一调用查看原始证据，填写结论后「保存调查视图」，再从左侧「已保存调查」重新打开，拆分数字由原始证据重新复算。

## API

- `POST /api/batches` `{format, filename, content}` 或 multipart `file` —— 导入来源批次（原始记录逐字保存）
- `GET /api/batches` —— 批次列表
- `GET /api/traces` —— 慢请求汇总（总耗时降序，含标记）
- `GET /api/traces/{traceId}` —— 重建因果树与每节点耗时拆分
- `POST /api/investigations` —— 保存调查视图，返回时即按原始证据重算
- `GET /api/investigations` / `GET /api/investigations/{id}` —— 列表 / 重新打开并复算
- `GET /api/formats` —— 已注册采集格式
