# 服务性能调查平台

Go + React 的最小可运行原型，用于导入人工调用记录并重建一次请求的因果关系。

## 能力

- 批次保留来源、格式、导入时间和不可变原始记录。
- 按 `traceId` 与 `parentId` 还原调用树，缺失父调用会作为悬挂根节点标出。
- 同设备子调用使用时间区间并集，并行重叠时间不会逐项相加。
- 跨设备时钟只保留可证明的持续时间和因果关系，页面展示名义值与执行/等待边界，不强凑精确排序。
- 重复记录单独标出并展示原始证据，不参与耗时求和。
- 调查保存批次 ID、请求 ID、选中节点、备注和视图状态；重新打开时从原始批次重新复算。
- 调用树支持逐节点展开/收起，展开状态随调查视图保存。
- 两次保存的调查可对照：总耗时增量拆分为本段处理、并行等待与证据不完整（不确定区间）。
- 研究者可补充时钟约束（A 结束必然早于 B 开始，或两条记录开始时刻之差落在某范围），
  约束换算为设备间时钟偏移区间后参与推算，原始时间戳不被修改。
- 每个调用展示开始、等待、结束的可能区间；约束不足或只有单向边界时明确标为无法界定。
- 约束相互矛盾时以异常形式点名涉及的约束与原记录，不自动选取任何精确时刻掩盖冲突。
- 时钟假设随调查保存，重开后按保存的假设复算；保存前可试用另一套假设而不影响已存结论。
- 对照两次调查时给出时钟假设差异说明、各请求等待比例区间；区间重叠的排名保留“无法判定先后”。

## 运行

```bash
cd frontend
npm install
npm run build
cd ..
PORT=8080 DATA_DIR=./data go run ./cmd/server
```

打开 http://localhost:8080 ，页面默认填入 `examples/manual-records.json` 等价示例，可直接导入。

## API

- `POST /api/import/manual`：导入人工调用 JSON。
- `GET /api/batches/{id}/analysis`：从批次原始记录重新计算分析。
- `POST /api/batches/{id}/analysis`：用请求体中的时钟约束试算分析，不修改已保存数据。
- `POST /api/investigations`：保存调查视图。
- `GET /api/investigations/{id}`：返回调查视图和重新计算后的分析。
- `GET /api/investigations/compare?a={id}&b={id}`：对照两次调查，返回耗时增量归因与不确定说明。

## 测试

```bash
go test ./...
cd frontend && npx vitest run --environment jsdom
```

当前人工格式版本为 `manual-calls-v1`，后续采集格式应新增独立 ingest adapter，不修改已保存批次的原始证据。
