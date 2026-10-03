import { useEffect, useMemo, useState } from 'react';
import { compareInvestigations, importManual, listInvestigations, openInvestigation, previewAnalysis, saveInvestigation } from './api';
import { sample } from './sample';
import type { Analysis, Anomaly, ClockConstraint, Comparison, Investigation, NodeType } from './types';
import './style.css';

const anomalyLabel: Record<string, string> = {
  duplicate_record: '重复记录', missing_parent: '缺失父调用', negative_duration: '时间倒置', multiple_roots: '多个根节点',
  constraint_conflict: '时钟约束矛盾', constraint_unknown_record: '约束引用缺失记录'
};

const attributionLabel: Record<string, string> = {
  execution: '总耗时增加主要来自本段处理',
  wait: '总耗时增加主要来自下游/并行等待',
  uncertain: '证据不完整，无法确定增量来源，保留区间',
  unchanged: '两次调查总耗时一致'
};

export default function App() {
  const [analysis, setAnalysis] = useState<Analysis | null>(null);
  const [payload, setPayload] = useState(sample);
  const [traceId, setTraceId] = useState('');
  const [selectedId, setSelectedId] = useState('');
  const [expanded, setExpanded] = useState<Set<string> | null>(null);
  const [name, setName] = useState('订单慢请求调查');
  const [notes, setNotes] = useState('');
  const [saved, setSaved] = useState<Investigation[]>([]);
  const [compareA, setCompareA] = useState('');
  const [compareB, setCompareB] = useState('');
  const [comparison, setComparison] = useState<Comparison | null>(null);
  const [message, setMessage] = useState('');
  const [constraints, setConstraints] = useState<ClockConstraint[]>([]);

  useEffect(() => { listInvestigations().then(setSaved).catch(() => undefined); }, []);
  const traces = analysis?.traces ?? {};
  const requests = analysis?.requests ?? [];
  const anomalies = analysis?.anomalies ?? [];
  const trace = (traceId && traces[traceId]) || traces[Object.keys(traces)[0]] || null;
  const selected = useMemo(() => trace ? findNode(trace, selectedId || trace.record.id) : null, [trace, selectedId]);
  const flatNodes = useMemo(() => trace ? flatten(trace) : [], [trace]);
  const expandedIds = expanded ?? new Set(flatNodes.map(node => node.record.id));
  const maxDuration = requests.reduce((max, item) => Math.max(max, item.durationMs || 0), 0);

  function adoptAnalysis(result: Analysis, preferredTrace?: string, preferredSelected?: string, expandedView?: string[]) {
    const first = (result.requests ?? [])[0];
    const traceKey = preferredTrace && result.traces?.[preferredTrace] ? preferredTrace : (first?.traceId || Object.keys(result.traces ?? {})[0] || '');
    setAnalysis(result);
    setTraceId(traceKey);
    setSelectedId(preferredSelected || first?.rootId || '');
    setExpanded(expandedView ? new Set(expandedView) : null);
  }

  async function doImport() {
    try {
      const result = await importManual(payload);
      adoptAnalysis(result);
      const count = (result.requests ?? []).length;
      setMessage(count > 0 ? `已导入批次 ${result.batch.id}` : `已导入批次 ${result.batch.id}，但没有可分析的调用记录`);
    } catch (error) { setMessage((error as Error).message); }
  }

  async function doSave() {
    if (!analysis || !trace) return;
    const item: Investigation = { batchId: analysis.batch.id, name, rootId: trace.record.id, traceId: trace.record.traceId, selectedId: selected?.record.id, notes, assumptions: constraints, view: { timeMode: 'bounds', expandedIds: [...expandedIds] } };
    const result = await saveInvestigation(item);
    setSaved(await listInvestigations());
    setMessage(`调查 ${result.id} 已保存`);
  }

  async function doPreview() {
    if (!analysis) return;
    try {
      const result = await previewAnalysis(analysis.batch.id, constraints);
      adoptAnalysis(result, traceId, selectedId, [...expandedIds]);
      setMessage('已按当前时钟假设试算，原始记录未改动；保存调查后假设随之保留');
    } catch (error) { setMessage((error as Error).message); }
  }

  async function reopen(id: string) {
    try {
      const reopened = await openInvestigation(id);
      adoptAnalysis(reopened.analysis, reopened.investigation.traceId, reopened.investigation.selectedId || reopened.investigation.rootId, reopened.investigation.view?.expandedIds);
      setName(reopened.investigation.name);
      setNotes(reopened.investigation.notes || '');
      setConstraints(reopened.investigation.assumptions ?? []);
      setMessage('已重新打开调查，耗时按保存的时钟假设由原始记录重新计算');
    } catch (error) { setMessage((error as Error).message); }
  }

  async function doCompare() {
    if (!compareA || !compareB) { setMessage('请选择两次调查进行对照'); return; }
    try {
      setComparison(await compareInvestigations(compareA, compareB));
      setMessage('');
    } catch (error) { setComparison(null); setMessage((error as Error).message); }
  }

  function toggle(id: string) {
    const next = new Set(expandedIds);
    if (next.has(id)) next.delete(id); else next.add(id);
    setExpanded(next);
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
        <ConstraintEditor constraints={constraints} onChange={setConstraints} onPreview={doPreview} disabled={!analysis} />
        <div className="compare">
          <h3>多次调查对照</h3>
          {saved.length < 2 && <p className="muted">保存至少两次调查后可对照</p>}
          {saved.length >= 2 && <>
            <label>基线<select value={compareA} onChange={e => setCompareA(e.target.value)}><option value="">选择调查</option>{saved.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>
            <label>对照<select value={compareB} onChange={e => setCompareB(e.target.value)}><option value="">选择调查</option>{saved.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>
            <button onClick={doCompare}>对照所选调查</button>
          </>}
          {comparison && <CompareResult comparison={comparison} />}
        </div>
      </aside>
      <section className="panel requests">
        <div className="panel-title"><h2>慢请求</h2><input value={name} onChange={e => setName(e.target.value)} /></div>
        {analysis && requests.length === 0 && <p className="muted">本批次没有可分析的调用记录。</p>}
        {requests.map(request => <button key={request.rootId} className={`request ${traceId === request.traceId && selectedId === request.rootId ? 'active' : ''}`} onClick={() => { setTraceId(request.traceId); setSelectedId(request.rootId); }}>
          <div><b>{request.service}</b><span>{request.operation}</span></div>
          <meter min={0} max={maxDuration || 1} value={request.durationMs} />
          <div className="numbers"><span>{fmt(request.durationMs)} ms</span><span>等待 {fmt(request.waitMs)}</span><span>执行 {fmt(request.executionMs)}</span></div>
          {request.anomalyCount > 0 && <em>{request.anomalyCount} 个异常</em>}
        </button>)}
        {analysis && <div className="anomalies"><h3>异常与不确定关系</h3>{anomalies.length === 0 && <p className="muted">无异常</p>}{anomalies.map((item, index) => <AnomalyRow key={index} item={item} />)}</div>}
        <label className="notes">调查备注<textarea value={notes} onChange={e => setNotes(e.target.value)} placeholder="记录结论、负责人或后续验证" /></label>
        <button className="primary" disabled={!analysis || !trace} onClick={doSave}>保存调查视图</button>
        {message && <p className="message">{message}</p>}
      </section>
      <section className="panel detail">
        <h2>因果关系与耗时拆分</h2>
        {!trace && <p className="muted">导入记录后显示请求树。</p>}
        {trace && <Timeline node={trace} selectedId={selected?.record.id || ''} onSelect={setSelectedId} expanded={expandedIds} onToggle={toggle} />}
        {selected && <Evidence node={selected} />}
        {analysis && <DuplicateEvidence analysis={analysis} />}
      </section>
    </section>
  </main>;
}

function BatchBadge({ analysis }: { analysis: Analysis }) {
  return <div className="badge"><b>{analysis.batch.source}</b><span>{analysis.batch.format}</span><span>{(analysis.batch.records ?? []).length} 条原始记录</span></div>;
}

function Timeline({ node, selectedId, onSelect, expanded, onToggle }: { node: NodeType; selectedId: string; onSelect: (id: string) => void; expanded: Set<string>; onToggle: (id: string) => void }) {
  const children = kids(node);
  const duration = node.timing?.durationMs || 0;
  const waitPct = duration > 0 ? Math.max(0, Math.min(100, (node.timing.waitMs?.nominal || 0) / duration * 100)) : 0;
  const isOpen = expanded.has(node.record.id);
  return <div className="tree">
    <div className="span-row" style={{ marginLeft: node.level * 18 }}>
      {children.length > 0
        ? <button className="toggle" aria-label={isOpen ? '收起' : '展开'} onClick={() => onToggle(node.record.id)}>{isOpen ? '▾' : '▸'}</button>
        : <span className="toggle-placeholder" />}
      <button className={`span ${selectedId === node.record.id ? 'active' : ''}`} onClick={() => onSelect(node.record.id)}>
        <div><b>{node.record.service}</b><span>{node.record.operation}</span><small>{node.record.deviceId}</small>{(node.record.anomalies ?? []).map(item => <em key={item}>{anomalyLabel[item] || item}</em>)}</div>
        <div className="bar"><i style={{ width: `${waitPct}%` }} /><span>{fmt(node.timing?.executionMs?.nominal)} 执行 / {fmt(node.timing?.waitMs?.nominal)} 等待 ms</span></div>
        {!node.timing?.boundsCertain && <small className="uncertain">跨设备时钟：{fmt(node.timing?.executionMs?.lower)}–{fmt(node.timing?.executionMs?.upper)} ms 执行（区间不确定）</small>}
        {node.timing?.parallel && <small className="parallel">并行区间已合并</small>}
      </button>
    </div>
    {isOpen && children.map(child => <Timeline key={child.record.id} node={child} selectedId={selectedId} onSelect={onSelect} expanded={expanded} onToggle={onToggle} />)}
  </div>;
}

function Evidence({ node }: { node: NodeType }) {
  return <article className="evidence">
    <h3>原始证据 · {node.record.id}</h3>
    <p>{node.timing?.reason || '该节点没有额外耗时说明。'}</p>
    <dl>
      <dt>开始区间（相对根）</dt><dd><IntervalText value={node.timing?.startMs} /></dd>
      <dt>结束区间（相对根）</dt><dd><IntervalText value={node.timing?.endMs} /></dd>
      <dt>等待区间</dt><dd><IntervalText value={node.timing?.waitMs} /></dd>
    </dl>
    <dl><dt>开始</dt><dd>{node.record.start}</dd><dt>结束</dt><dd>{node.record.end}</dd><dt>父调用</dt><dd>{node.record.parentId || '根调用'}</dd><dt>设备</dt><dd>{node.record.deviceId}</dd></dl>
    <pre>{JSON.stringify(node.record.raw || node.record, null, 2)}</pre>
  </article>;
}

function IntervalText({ value }: { value?: { nominal: number; lower?: number; upper?: number; certain: boolean; known?: boolean; reason?: string } }) {
  if (!value) return <span>—</span>;
  if (value.certain) return <span>{fmt(value.nominal)} ms（确定）</span>;
  if (value.known === false) return <span className="uncertain">{value.reason || '区间无法界定'}</span>;
  return <span>{fmt(value.lower)}–{fmt(value.upper)} ms（名义 {fmt(value.nominal)}）</span>;
}

function ConstraintEditor({ constraints, onChange, onPreview, disabled }: { constraints: ClockConstraint[]; onChange: (items: ClockConstraint[]) => void; onPreview: () => void; disabled: boolean }) {
  function update(index: number, patch: Partial<ClockConstraint>) {
    onChange(constraints.map((item, i) => i === index ? { ...item, ...patch } : item));
  }
  function add() {
    onChange([...constraints, { id: `c-${Date.now()}`, type: 'order', aRecordId: '', bRecordId: '' }]);
  }
  return <div className="constraints">
    <h3>时钟假设（约束）</h3>
    <p className="muted">补充记录间的先后或时差范围；矛盾时会点名涉及的原记录，不会自动选时刻。</p>
    {constraints.map((item, index) => <div className="constraint" key={item.id}>
      <select value={item.type} onChange={e => update(index, { type: e.target.value as ClockConstraint['type'] })}>
        <option value="order">A 结束早于 B 开始</option>
        <option value="offset_range">B.start − A.start 范围</option>
      </select>
      <input placeholder="记录 A" value={item.aRecordId} onChange={e => update(index, { aRecordId: e.target.value })} />
      <input placeholder="记录 B" value={item.bRecordId} onChange={e => update(index, { bRecordId: e.target.value })} />
      {item.type === 'offset_range' && <>
        <input type="number" placeholder="下限 ms" value={item.lowerMs ?? ''} onChange={e => update(index, { lowerMs: e.target.value === '' ? undefined : Number(e.target.value) })} />
        <input type="number" placeholder="上限 ms" value={item.upperMs ?? ''} onChange={e => update(index, { upperMs: e.target.value === '' ? undefined : Number(e.target.value) })} />
      </>}
      <button className="ghost" onClick={() => onChange(constraints.filter((_, i) => i !== index))}>删除</button>
    </div>)}
    <div className="constraint-actions">
      <button className="ghost" onClick={add}>添加约束</button>
      <button disabled={disabled} onClick={onPreview}>试用当前假设</button>
    </div>
  </div>;
}

function DuplicateEvidence({ analysis }: { analysis: Analysis }) {
  const items = (analysis.batch.records ?? []).filter(record => record.duplicate || (record.anomalies ?? []).includes('duplicate_record'));
  if (items.length === 0) return null;
  return <div className="evidence"><h3>重复记录原始证据</h3>{items.map(record => <details key={`${record.id}-${record.start}`}><summary>{record.id} · {record.service} · {record.operation}</summary><pre>{JSON.stringify(record.raw || record, null, 2)}</pre></details>)}</div>;
}

function CompareResult({ comparison }: { comparison: Comparison }) {
  const delta = comparison.durationDeltaMs;
  return <div className="compare-result">
    <h4>对照结果</h4>
    <p><b>{comparison.a.name}</b> → <b>{comparison.b.name}</b>：总耗时 {delta >= 0 ? '+' : ''}{fmt(delta)} ms</p>
    <dl>
      <dt>本段处理增量</dt><dd><DeltaValue value={comparison.executionDelta} /></dd>
      <dt>等待（含并行）增量</dt><dd><DeltaValue value={comparison.waitDelta} /></dd>
    </dl>
    <p className={comparison.evidenceIncomplete ? 'uncertain' : ''}>{attributionLabel[comparison.attribution] || comparison.attribution}</p>
    {comparison.assumptionChange && <small className="muted note">{comparison.assumptionChange}</small>}
    {comparison.evidenceIncomplete && <small className="uncertain">证据不完整：存在异常记录或跨设备时钟不确定</small>}
    {(comparison.rankingNotes ?? []).map((note, index) => <small key={`rank-${index}`} className="uncertain note">{note}</small>)}
    {(comparison.requests ?? []).length > 0 && <div className="request-compare">
      <h4>各请求等待比例（区间重叠则无法判定先后）</h4>
      {(comparison.requests ?? []).map(item => <p key={item.traceId} className={item.orderUndecidable ? 'uncertain' : ''}>
        <b>{item.service}</b> {item.operation}：基线 <ShareText value={item.waitShareA} />，对照 <ShareText value={item.waitShareB} />
        {item.orderUndecidable && <em>无法判定先后</em>}
      </p>)}
    </div>}
    {(comparison.notes ?? []).map((note, index) => <small key={index} className="muted note">{note}</small>)}
  </div>;
}

function ShareText({ value }: { value: { nominal: number; lower?: number; upper?: number; certain: boolean; known?: boolean } }) {
  if (value.known === false) return <span>无法界定</span>;
  const pct = (v?: number) => `${Math.round((v ?? 0) * 100)}%`;
  if (value.certain) return <span>{pct(value.nominal)}</span>;
  return <span>{pct(value.lower)}–{pct(value.upper)}</span>;
}

function DeltaValue({ value }: { value: { nominal: number; lower?: number; upper?: number; certain: boolean } }) {
  if (!value.certain) return <span>{fmt(value.lower)}–{fmt(value.upper)} ms（名义 {fmt(value.nominal)}）</span>;
  return <span>{fmt(value.nominal)} ms</span>;
}

function AnomalyRow({ item }: { item: Anomaly }) { return <p className="anomaly"><b>{anomalyLabel[item.type] || item.type}</b><span>{item.summary}</span></p>; }
function kids(node: NodeType): NodeType[] { return node.children ?? []; }
function flatten(node: NodeType): NodeType[] { return [node, ...kids(node).flatMap(flatten)]; }
function findNode(node: NodeType, id: string): NodeType | null { if (node.record.id === id) return node; for (const child of kids(node)) { const found = findNode(child, id); if (found) return found; } return null; }
function fmt(value: number | undefined) { return Number.isFinite(value) ? Math.round((value || 0) * 100) / 100 : '—'; }
