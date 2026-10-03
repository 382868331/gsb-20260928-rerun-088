import { cleanup, render, screen, fireEvent, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, test } from 'vitest';
import App from './App';

const record = (id: string, extra: Record<string, unknown> = {}) => ({
  id, traceId: 't-1', service: 'svc', operation: 'op', deviceId: 'host-a',
  start: '2026-09-29T10:00:00Z', end: '2026-09-29T10:00:01Z',
  durationMs: { nominal: 1000, certain: true }, ...extra
});

const node = (id: string, children: unknown): unknown => ({
  record: record(id),
  children,
  timing: { durationMs: 1000, waitMs: { nominal: 600, lower: 600, upper: 600, certain: true }, executionMs: { nominal: 400, lower: 400, upper: 400, certain: true }, boundsCertain: true, parallel: false },
  level: 0
});

const baseAnalysis = {
  batch: { id: 'batch-1', source: 'test', format: 'manual-calls-v1', importedAt: '2026-09-29T10:00:00Z', records: [record('root')] },
  requests: [{ rootId: 'root', traceId: 't-1', service: 'svc', operation: 'op', start: '2026-09-29T10:00:00Z', durationMs: 1000, waitMs: 600, executionMs: 400, spanCount: 1, anomalyCount: 0, certain: true }],
  traces: { 't-1': node('root', [node('child', null)]) },
  anomalies: [],
  generatedAt: '2026-09-29T10:00:00Z'
};

function mockFetch(analysis: unknown, investigations: unknown[] = []) {
  globalThis.fetch = (async (url: unknown, init?: RequestInit) => {
    const target = String(url);
    if (target.includes('/clock-analysis')) return new Response(JSON.stringify(analysis), { status: 200 });
    if (target.startsWith('/api/import')) return new Response(JSON.stringify(analysis), { status: 200 });
    if (target.includes('/api/investigations/compare')) return new Response(JSON.stringify({
      a: { name: '第一次' }, b: { name: '第二次' }, durationDeltaMs: 0,
      executionDelta: { nominal: 0, certain: true }, waitDelta: { nominal: 0, certain: true },
      attribution: 'unchanged', evidenceIncomplete: false, notes: []
    }), { status: 200 });
    if (target.startsWith('/api/investigations') && !target.includes('compare')) {
      if (init?.method === 'POST') return new Response(JSON.stringify({ id: 'inv-new' }), { status: 200 });
      return new Response(JSON.stringify(investigations), { status: 200 });
    }
    return new Response('{}', { status: 200 });
  }) as unknown as typeof fetch;
}

beforeEach(() => { mockFetch(baseAnalysis); });
afterEach(cleanup);

test('导入后渲染请求树，叶子 children 为 null 不白屏', async () => {
  render(<App />);
  fireEvent.click(screen.getByText('导入并分析'));
  await waitFor(() => screen.getByText(/已导入批次 batch-1/));
  screen.getByText('因果关系与耗时拆分');
  screen.getByText('原始证据 · root');
});

test('空集合（无请求、无异常）进入界面不白屏', async () => {
  mockFetch({ ...baseAnalysis, requests: [], traces: {}, anomalies: [], batch: { ...baseAnalysis.batch, records: [] } });
  render(<App />);
  fireEvent.click(screen.getByText('导入并分析'));
  await waitFor(() => screen.getByText(/已导入批次 batch-1/));
  screen.getByText('本批次没有可分析的调用记录。');
  screen.getByText('导入记录后显示请求树。');
});

test('展开/收起任意节点不破坏调查', async () => {
  render(<App />);
  fireEvent.click(screen.getByText('导入并分析'));
  await waitFor(() => screen.getByText(/已导入批次/));
  const toggles = screen.getAllByRole('button', { name: '收起' });
  fireEvent.click(toggles[0]);
  await waitFor(() => screen.getByRole('button', { name: '展开' }));
  fireEvent.click(screen.getByRole('button', { name: '展开' }));
  await waitFor(() => screen.getByText('原始证据 · root'));
});

test('保存调查后重算并支持对照', async () => {
  const investigations = [
    { id: 'inv-1', batchId: 'batch-1', name: '第一次', rootId: 'root', traceId: 't-1' },
    { id: 'inv-2', batchId: 'batch-1', name: '第二次', rootId: 'root', traceId: 't-1' }
  ];
  mockFetch(baseAnalysis, investigations);
  render(<App />);
  fireEvent.click(screen.getByText('导入并分析'));
  await waitFor(() => screen.getByText(/已导入批次/));
  fireEvent.click(screen.getByText('保存调查视图'));
  await waitFor(() => screen.getByText(/调查 inv-new 已保存/));
  fireEvent.change(screen.getByLabelText('基线'), { target: { value: 'inv-1' } });
  fireEvent.change(screen.getByLabelText('对照'), { target: { value: 'inv-2' } });
  fireEvent.click(screen.getByText('对照所选调查'));
  await waitFor(() => screen.getByText('两次调查总耗时一致'));
});

test('约束分析：矛盾假设展示冲突约束与涉及记录', async () => {
  const conflictAnalysis = {
    ...baseAnalysis,
    clock: {
      feasible: false,
      conflicts: [{ type: 'contradiction', constraintIds: ['c-1', 'c-2'], recordIds: ['root', 'child'], summary: '时钟约束相互矛盾：不存在能同时满足全部依据的时刻' }],
      uncertainRecordIds: ['child']
    }
  };
  mockFetch(conflictAnalysis);
  render(<App />);
  fireEvent.click(screen.getByText('导入并分析'));
  await waitFor(() => screen.getByText(/已导入批次/));
  fireEvent.click(screen.getByText('添加约束'));
  fireEvent.change(screen.getByLabelText('基准记录'), { target: { value: 'root' } });
  fireEvent.change(screen.getByLabelText('目标记录'), { target: { value: 'child' } });
  fireEvent.click(screen.getByText('运行约束分析'));
  await waitFor(() => screen.getByText(/假设不可行/));
  screen.getByText('约束相互矛盾');
  screen.getByText(/涉及约束：c-1、c-2/);
  screen.getByText(/涉及记录：root、child/);
  screen.getByText(/仍无法确定相对顺序的记录：child/);
});

test('约束可暂时停用并随调查保存', async () => {
  let savedBody: string | null = null;
  globalThis.fetch = (async (url: unknown, init?: RequestInit) => {
    const target = String(url);
    if (target.includes('/clock-analysis')) return new Response(JSON.stringify({ ...baseAnalysis, clock: { feasible: true, disabledConstraintIds: ['c-x'] } }), { status: 200 });
    if (target.startsWith('/api/import')) return new Response(JSON.stringify(baseAnalysis), { status: 200 });
    if (target.startsWith('/api/investigations')) {
      if (init?.method === 'POST') { savedBody = String(init.body); return new Response(JSON.stringify({ id: 'inv-new' }), { status: 200 }); }
      return new Response(JSON.stringify([]), { status: 200 });
    }
    return new Response('{}', { status: 200 });
  }) as unknown as typeof fetch;
  render(<App />);
  fireEvent.click(screen.getByText('导入并分析'));
  await waitFor(() => screen.getByText(/已导入批次/));
  fireEvent.click(screen.getByText('添加约束'));
  fireEvent.click(screen.getByLabelText('启用'));
  fireEvent.click(screen.getByText('运行约束分析'));
  await waitFor(() => screen.getByText(/已停用约束：c-x/));
  fireEvent.click(screen.getByText('保存调查视图'));
  await waitFor(() => screen.getByText(/调查 inv-new 已保存/));
  const saved = JSON.parse(savedBody || '{}');
  if (!saved.hypothesis?.constraints?.[0]?.disabled) throw new Error('停用的约束必须随调查保存');
});
