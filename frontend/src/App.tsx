import { useEffect, useMemo, useState } from 'react';
import { analyzeClock, compareInvestigations, importManual, listInvestigations, openInvestigation, saveInvestigation } from './api';
import { sample } from './sample';
import type { Analysis, Anomaly, ClockConstraint, Comparison, Investigation, NodeType, OffsetBound } from './types';
import './style.css';

const anomalyLabel: Record<string, string> = {
  duplicate_record: '重复记录', missing_parent: '缺失父调用', negative_duration: '时间倒置', multiple_roots: '多个根节点'
};

const attributionLabel: Record<string, string> = {
  execution: '总耗时增加主要来自本段处理',
  wait: '总耗时增加主要来自下游/并行等待',
  uncertain: '证据不完整，无法确定增量来源，保留区间',
  unchanged: '两次调查总耗时一致'
};

attributionLabel.hypothesis = '差异来自时钟假设收紧，而非真实耗时变化';

const conflictLabel: Record<string, string> = {
  contradiction: '约束相互矛盾',
  invalid_constraint: '约束无效',
  unknown_record: '引用未知记录',
  invalid_range: '时差范围不成立',
  excluded_record: '引用已停用记录'
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
  const [hypothesisName, setHypothesisName] = useState('');
  const [constraints, setConstraints] = useState<ClockConstraint[]>([]);
  const [disabledRecords, setDisabledRecords] = useState<string[]>([]);

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
    const item: Investigation = { batchId: analysis.batch.id, name, rootId: trace.record.id, traceId: trace.record.traceId, selectedId: selected?.record.id, notes, view: { timeMode: 'bounds', expandedIds: [...expandedIds] }, hypothesis: { name: hypothesisName, constraints, disabledRecords } };
    const result = await saveInvestigation(item);
    setSaved(await listInvestigations());
    setMessage(`调查 ${result.id} 已保存`);
  }

  async function reopen(id: string) {
    try {
      const reopened = await openInvestigation(id);
      adoptAnalysis(reopened.analysis, reopened.investigation.traceId, reopened.investigation.selectedId || reopened.investigation.rootId, reopened.investigation.view?.expandedIds);
      setName(reopened.investigation.name);
      setNotes(reopened.investigation.notes || '');
      const hypothesis = reopened.investigation.hypothesis;
      setHypothesisName(hypothesis?.name || '');
      setConstraints(hypothesis?.constraints ?? []);
      setDisabledRecords(hypothesis?.disabledRecords ?? []);
      setMessage('已重新打开调查，耗时由原始记录重新计算');
    } catch (error) { setMessage((error as Error).message); }
  }

  async function doCompare() {
    if (!compareA || !compareB) { setMessage('请选择两次调查进行对照'); return; }
    try {
      setComparison(await compareInvestigations(compareA, compareB));
      setMessage('');
    } catch (error) { setComparison(null); setMessage((error as Error).message); }
  }

  async function applyHypothesis() {
    if (!analysis) return;
    try {
      const result = await analyzeClock(analysis.batch.id, { name: hypothesisName, constraints, disabledRecords });
      adoptAnalysis(result, traceId, selectedId, [...expandedIds]);
      setMessage('已按时钟假设重新求解，原始时间戳未被修改');
    } catch (error) { setMessage((error as Error).message); }
  }

  function updateConstraint(id: string, patch: Partial<ClockConstraint>) {
    setConstraints(prev => prev.map(item => item.id === id ? { ...item, ...patch } : item));
  }

  function addConstraint() {
    const id = `c-${constraints.length + 1}`;
    setConstraints(prev => [...prev, { id, type: 'offset', anchor: '', target: '', minMs: 0, maxMs: 0, rationale: '' }]);
  }

  function toggleRecordDisabled(recordId: string) {
    setDisabledRecords(prev => prev.includes(recordId) ? prev.filter(item => item !== recordId) : [...prev, recordId]);
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
        {analysis && <ClockSection
          analysis={analysis}
          hypothesisName={hypothesisName}
          setHypothesisName={setHypothesisName}
          constraints={constraints}
          addConstraint={addConstraint}
          updateConstraint={updateConstraint}
          removeConstraint={(id: string) => setConstraints(prev => prev.filter(item => item.id !== id))}
          disabledRecords={disabledRecords}
          toggleRecordDisabled={toggleRecordDisabled}
          applyHypothesis={applyHypothesis}
        />}
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
    <dl><dt>开始</dt><dd>{node.record.start}</dd><dt>结束</dt><dd>{node.record.end}</dd><dt>父调用</dt><dd>{node.record.parentId || '根调用'}</dd><dt>设备</dt><dd>{node.record.deviceId}</dd></dl>
    <pre>{JSON.stringify(node.record.raw || node.record, null, 2)}</pre>
  </article>;
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
    {(comparison.hypothesisA || comparison.hypothesisB) && <p className="muted">假设：{comparison.hypothesisA || '（无）'} → {comparison.hypothesisB || '（无）'}</p>}
    {comparison.assumptionTightened && <p className="uncertain">同一批次的不同假设：耗时区间变化来自假设收紧，不是真实耗时变化</p>}
    <dl>
      <dt>本段处理增量</dt><dd><DeltaValue value={comparison.executionDelta} /></dd>
      <dt>等待（含并行）增量</dt><dd><DeltaValue value={comparison.waitDelta} /></dd>
    </dl>
    <p className={comparison.evidenceIncomplete ? 'uncertain' : ''}>{attributionLabel[comparison.attribution] || comparison.attribution}</p>
    {comparison.evidenceIncomplete && <small className="uncertain">证据不完整：存在异常记录或跨设备时钟不确定</small>}
    {(comparison.notes ?? []).map((note, index) => <small key={index} className="muted note">{note}</small>)}
  </div>;
}

type ClockSectionProps = {
  analysis: Analysis;
  hypothesisName: string;
  setHypothesisName: (value: string) => void;
  constraints: ClockConstraint[];
  addConstraint: () => void;
  updateConstraint: (id: string, patch: Partial<ClockConstraint>) => void;
  removeConstraint: (id: string) => void;
  disabledRecords: string[];
  toggleRecordDisabled: (id: string) => void;
  applyHypothesis: () => void;
};
function ClockSection(props: ClockSectionProps) {
  const clock = props.analysis.clock;
  const records = (props.analysis.batch.records ?? []).filter(record => !record.duplicate);
  return <div className="evidence clock-section">
    <h3>时钟假设与约束分析</h3>
    <label className="field">假设名称
      <input value={props.hypothesisName} onChange={event => props.setHypothesisName(event.target.value)} placeholder="例如：host-b 时钟快 200ms" />
    </label>
    {props.constraints.length === 0 && <p className="muted">尚未添加时钟依据。用 记录ID:start / 记录ID:end 引用原始事件。</p>}
    {props.constraints.map(item => <div key={item.id} className={item.disabled ? 'constraint disabled' : 'constraint'}>
      <div className="constraint-head">
        <b>{item.id}</b>
        <select value={item.type} onChange={event => props.updateConstraint(item.id, { type: event.target.value })}>
          <option value="offset">时差范围</option>
          <option value="before">先后关系</option>
        </select>
        <label><input type="checkbox" checked={!!item.disabled} onChange={event => props.updateConstraint(item.id, { disabled: event.target.checked })} /> 暂时停用</label>
        <button className="ghost danger" onClick={() => props.removeConstraint(item.id)}>删除</button>
      </div>
      {item.type === 'offset'
        ? <div className="constraint-refs">
            <input value={item.anchor || ''} placeholder="基准事件 anchor:start" onChange={event => props.updateConstraint(item.id, { anchor: event.target.value })} />
            <input value={item.target || ''} placeholder="目标事件 target:start" onChange={event => props.updateConstraint(item.id, { target: event.target.value })} />
          </div>
        : <div className="constraint-refs">
            <input value={item.before || ''} placeholder="先发生事件 a:end" onChange={event => props.updateConstraint(item.id, { before: event.target.value })} />
            <input value={item.after || ''} placeholder="后发生事件 b:start" onChange={event => props.updateConstraint(item.id, { after: event.target.value })} />
          </div>}
      <div className="constraint-refs">
        <input type="number" value={item.minMs} onChange={event => props.updateConstraint(item.id, { minMs: Number(event.target.value) })} placeholder="最小 ms" />
        <input type="number" value={item.maxMs} onChange={event => props.updateConstraint(item.id, { maxMs: Number(event.target.value) })} placeholder="最大 ms" />
        <input value={item.rationale || ''} placeholder="依据来源（日志、对时记录）" onChange={event => props.updateConstraint(item.id, { rationale: event.target.value })} />
      </div>
    </div>)}
    <div className="constraint-actions">
      <button className="ghost" onClick={props.addConstraint}>新增时钟依据</button>
      <button onClick={props.applyHypothesis}>应用假设求解</button>
    </div>
    {clock && <div className="clock-report">
      <p className={clock.feasible ? 'feasible' : 'uncertain'}>{clock.feasible ? '当前假设可成立：所有约束可同时满足。' : '存在无法同时成立的约束，系统未挑选任何精确时刻掩盖冲突。'}</p>
      {(clock.conflicts ?? []).map((conflict, index) => <p key={index} className="anomaly">
        <b>{conflictLabel[conflict.type] || conflict.type}</b>
        <span>{conflict.summary}</span>
        <small>约束：{(conflict.constraintIds ?? []).join(', ') || '—'} · 记录：{(conflict.recordIds ?? []).join(', ') || '—'}</small>
      </p>)}
      {(clock.inactiveConstraintIds ?? []).length > 0 && <p className="muted">已停用约束：{(clock.inactiveConstraintIds ?? []).join(', ')}（未参与求解，可随时恢复）</p>}
      {(clock.uncertainRecordIds ?? []).length > 0 && <p className="uncertain">仍无法确定区间的记录：{(clock.uncertainRecordIds ?? []).join(', ')}</p>}
      <h4>各记录相对根节点的起止区间（ms）</h4>
      <table className="offsets">
        <thead><tr><th>记录</th><th>设备</th><th>开始区间</th><th>结束区间</th><th>证据</th></tr></thead>
        <tbody>
          {(clock.offsets ?? []).map(bound => <OffsetRow key={bound.recordId} bound={bound} disabled={props.disabledRecords.includes(bound.recordId)} onToggle={() => props.toggleRecordDisabled(bound.recordId)} />)}
          {(clock.offsets ?? []).length === 0 && records.map(record => <tr key={record.id}><td>{record.id}</td><td>{record.deviceId}</td><td colSpan={2} className="muted">无区间数据</td><td /></tr>)}
        </tbody>
      </table>
    </div>}
  </div>;
}
function OffsetRow({ bound, disabled, onToggle }: { bound: OffsetBound; disabled: boolean; onToggle: () => void }) {
  const range = (lower: number, upper: number) => bound.certain ? `${fmt(lower)} – ${fmt(upper)}` : '无界（仅名义读数）';
  return <tr className={disabled ? 'disabled' : ''}>
    <td>{bound.recordId}</td>
    <td>{bound.deviceId}</td>
    <td>{disabled ? '已停用' : range(bound.startLower, bound.startUpper)}</td>
    <td>{disabled ? '已停用' : range(bound.endLower, bound.endUpper)}</td>
    <td><button className="ghost" onClick={onToggle}>{disabled ? '恢复证据' : '停用证据'}</button></td>
  </tr>;
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
