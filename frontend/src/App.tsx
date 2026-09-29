import { useEffect, useMemo, useState } from 'react';
import { getAnalysis, importManual, listInvestigations, openInvestigation, saveInvestigation } from './api';
import { sample } from './sample';
import type { Analysis, Anomaly, Investigation, NodeType } from './types';
import './style.css';

const anomalyLabel: Record<string, string> = {
  duplicate_record: '重复记录', missing_parent: '缺失父调用', negative_duration: '时间倒置', multiple_roots: '多个根节点'
};

export default function App() {
  const [analysis, setAnalysis] = useState<Analysis | null>(null);
  const [payload, setPayload] = useState(sample);
  const [traceId, setTraceId] = useState('');
  const [selectedId, setSelectedId] = useState('');
  const [name, setName] = useState('订单慢请求调查');
  const [notes, setNotes] = useState('');
  const [saved, setSaved] = useState<Investigation[]>([]);
  const [message, setMessage] = useState('');

  useEffect(() => { listInvestigations().then(setSaved).catch(() => undefined); }, []);
  const trace = analysis?.traces[traceId] || analysis?.traces[Object.keys(analysis.traces)[0]] || null;
  const selected = useMemo(() => trace ? findNode(trace, selectedId || trace.record.id) : null, [trace, selectedId]);
  const flatNodes = useMemo(() => trace ? flatten(trace) : [], [trace]);

  async function doImport() {
    try {
      const result = await importManual(payload);
      const first = result.requests[0];
      setAnalysis(result);
      setTraceId(first?.traceId || '');
      setSelectedId(first?.rootId || '');
      setMessage(`已导入批次 ${result.batch.id}`);
    } catch (error) { setMessage((error as Error).message); }
  }

  async function doSave() {
    if (!analysis || !trace) return;
    const item: Investigation = { batchId: analysis.batch.id, name, rootId: trace.record.id, traceId: trace.record.traceId, selectedId: selected?.record.id, notes, view: { timeMode: 'bounds', expandedIds: flatNodes.map(node => node.record.id) } };
    const result = await saveInvestigation(item);
    setSaved(await listInvestigations());
    setMessage(`调查 ${result.id} 已保存`);
  }

  async function reopen(id: string) {
    const reopened = await openInvestigation(id);
    setAnalysis(reopened.analysis);
    setTraceId(reopened.investigation.traceId);
    setName(reopened.investigation.name);
    setNotes(reopened.investigation.notes || '');
    setSelectedId(reopened.investigation.selectedId || reopened.investigation.rootId);
    setMessage(`已重新打开调查，耗时由原始记录重新计算`);
  }

  return <main>
    <header><div><h1>服务性能调查平台</h1><p>导入人工调用记录，复建一次请求的因果链，区分等待与实际执行。</p></div>{analysis && <BatchBadge analysis={analysis} />}</header>
    <section className="grid">
      <aside className="panel import">
        <h2>来源批次</h2>
        <textarea value={payload} onChange={event => setPayload(event.target.value)} spellCheck={false} />
        <button onClick={doImport}>导入并分析</button>
        <div className="saved">
          <h3>已保存调查</h3>
          {saved.length === 0 && <p className="muted">尚无保存视图</p>}
          {saved.map(item => <button key={item.id} className="ghost" onClick={() => item.id && reopen(item.id)}>{item.name}<span>{item.id}</span></button>)}
        </div>
      </aside>
      <section className="panel requests">
        <div className="panel-title"><h2>慢请求</h2><input value={name} onChange={e => setName(e.target.value)} /></div>
        {analysis?.requests.map(request => <button key={request.rootId} className={`request ${traceId === request.traceId && selectedId === request.rootId ? 'active' : ''}`} onClick={() => { setTraceId(request.traceId); setSelectedId(request.rootId); }}>
          <div><b>{request.service}</b><span>{request.operation}</span></div>
          <meter min={0} max={Math.max(...analysis.requests.map(item => item.durationMs))} value={request.durationMs} />
          <div className="numbers"><span>{fmt(request.durationMs)} ms</span><span>等待 {fmt(request.waitMs)}</span><span>执行 {fmt(request.executionMs)}</span></div>
          {request.anomalyCount > 0 && <em>{request.anomalyCount} 个异常</em>}
        </button>)}
        {analysis && <div className="anomalies"><h3>异常与不确定关系</h3>{analysis.anomalies.map((item, index) => <AnomalyRow key={index} item={item} />)}</div>}
        <label className="notes">调查备注<textarea value={notes} onChange={e => setNotes(e.target.value)} placeholder="记录结论、负责人或后续验证" /></label>
        <button className="primary" disabled={!analysis} onClick={doSave}>保存调查视图</button>
        {message && <p className="message">{message}</p>}
      </section>
      <section className="panel detail">
        <h2>因果关系与耗时拆分</h2>
        {!trace && <p className="muted">导入记录后显示请求树。</p>}
        {trace && <Timeline node={trace} selectedId={selected?.record.id || ''} onSelect={setSelectedId} />}
        {selected && <Evidence node={selected} />}
        {analysis && <DuplicateEvidence analysis={analysis} />}
      </section>
    </section>
  </main>;
}

function BatchBadge({ analysis }: { analysis: Analysis }) {
  return <div className="badge"><b>{analysis.batch.source}</b><span>{analysis.batch.format}</span><span>{analysis.batch.records.length} 条原始记录</span></div>;
}

function Timeline({ node, selectedId, onSelect }: { node: NodeType; selectedId: string; onSelect: (id: string) => void }) {
  const waitPct = Math.max(0, Math.min(100, node.timing.waitMs.nominal / node.timing.durationMs * 100));
  return <div className="tree">
    <button className={`span ${selectedId === node.record.id ? 'active' : ''}`} style={{ marginLeft: node.level * 18 }} onClick={() => onSelect(node.record.id)}>
      <div><b>{node.record.service}</b><span>{node.record.operation}</span><small>{node.record.deviceId}</small>{node.record.anomalies?.map(item => <em key={item}>{anomalyLabel[item] || item}</em>)}</div>
      <div className="bar"><i style={{ width: `${waitPct}%` }} /><span>{fmt(node.timing.executionMs.nominal)} 执行 / {fmt(node.timing.waitMs.nominal)} 等待 ms</span></div>
      {!node.timing.boundsCertain && <small className="uncertain">跨设备时钟：{fmt(node.timing.executionMs.lower)}–{fmt(node.timing.executionMs.upper)} ms 执行</small>}
      {node.timing.parallel && <small className="parallel">并行区间已合并</small>}
    </button>
    {node.children.map(child => <Timeline key={child.record.id} node={child} selectedId={selectedId} onSelect={onSelect} />)}
  </div>;
}

function Evidence({ node }: { node: NodeType }) {
  return <article className="evidence">
    <h3>原始证据 · {node.record.id}</h3>
    <p>{node.timing.reason}</p>
    <dl><dt>开始</dt><dd>{node.record.start}</dd><dt>结束</dt><dd>{node.record.end}</dd><dt>父调用</dt><dd>{node.record.parentId || '根调用'}</dd><dt>设备</dt><dd>{node.record.deviceId}</dd></dl>
    <pre>{JSON.stringify(node.record.raw || node.record, null, 2)}</pre>
  </article>;
}

function DuplicateEvidence({ analysis }: { analysis: Analysis }) {
  const items = analysis.batch.records.filter(record => record.duplicate || record.anomalies?.includes('duplicate_record'));
  if (items.length === 0) return null;
  return <div className="evidence"><h3>重复记录原始证据</h3>{items.map(record => <details key={`${record.id}-${record.start}`}><summary>{record.id} · {record.service} · {record.operation}</summary><pre>{JSON.stringify(record.raw || record, null, 2)}</pre></details>)}</div>;
}

function AnomalyRow({ item }: { item: Anomaly }) { return <p className="anomaly"><b>{anomalyLabel[item.type] || item.type}</b><span>{item.summary}</span></p>; }
function flatten(node: NodeType): NodeType[] { return [node, ...node.children.flatMap(flatten)]; }
function findNode(node: NodeType, id: string): NodeType | null { if (node.record.id === id) return node; for (const child of node.children) { const found = findNode(child, id); if (found) return found; } return null; }
function fmt(value: number | undefined) { return Number.isFinite(value) ? Math.round((value || 0) * 100) / 100 : '—'; }
