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
- 调查可保存时钟假设（时差范围/先后关系约束），约束只引用原始记录事件、从不修改原始时间戳。
- 约束分析把跨设备区间按启用假设收紧并标记为假设推导；矛盾假设列出构成冲突的约束链与涉及记录，回退到仅设备时钟的区间。
- 单条约束可暂时停用再恢复，停用状态随调查保存；无法确定相对顺序的记录明确列出，不强行排名。
- 对照同一批次的两套假设时，区分真实耗时变化与仅由假设收紧造成的区间变化。
- 调查可导出复核包（原始记录、启用与停用的时钟依据、冲突链、调查对照结论）；另一台同版本应用导入后按原依据重算，原保存结论与重新求解结果并列返回，缺失记录或重复 ID 列入问题清单，依据不静默丢弃。

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
- `POST /api/batches/{id}/clock-analysis`：以请求体中的时钟假设做约束分析，返回可行性与矛盾链。
- `POST /api/investigations`：保存调查视图。
- `GET /api/investigations/{id}`：返回调查视图和重新计算后的分析。
- `GET /api/investigations/compare?a={id}&b={id}`：对照两次调查，返回耗时增量归因与不确定说明。
- `GET /api/investigations/{id}/review-bundle?compareWith={id}`：导出调查复核包，可选嵌入与另一调查的对照结论。
- `POST /api/review-bundles/import`：导入复核包（`review-bundle-v1`），持久化批次与调查，返回原保存结论、重新求解结果与问题清单。

## 测试

```bash
go test ./...
cd frontend && npx vitest run --environment jsdom
```

当前人工格式版本为 `manual-calls-v1`，后续采集格式应新增独立 ingest adapter，不修改已保存批次的原始证据。
