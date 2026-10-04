import { useEffect, useMemo, useState, type ChangeEvent } from 'react';
import { compareInvestigations, exportReview, importManual, importReview, listInvestigations, openInvestigation, runClockAnalysis, saveInvestigation } from './api';
import { sample } from './sample';
import type { Analysis, Anomaly, ClockConstraint, ClockReport, Comparison, Investigation, NodeType, ReviewImportResult, ReviewPackage } from './types';
import './style.css';

const anomalyLabel: Record<string, string> = {
  duplicate_record: '重复记录', missing_parent: '缺失父调用', negative_duration: '时间倒置', multiple_roots: '多个根节点'
};

const conflictLabel: Record<string, string> = {
  contradiction: '约束相互矛盾', invalid_constraint: '约束无效', unknown_record: '引用未知记录', invalid_range: '时差范围不成立'
};

const attributionLabel: Record<string, string> = {
  execution: '总耗时增加主要来自本段处理',
  wait: '总耗时增加主要来自下游/并行等待',
  uncertain: '证据不完整，无法确定增量来源，保留区间',
  unchanged: '两次调查总耗时一致'
};

let constraintSeq = 1;
function newConstraint(): ClockConstraint {
  return { id: `c-${constraintSeq++}`, type: 'offset', anchor: '', target: '', minMs: 0, maxMs: 0, rationale: '' };
}

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
  const [reviewSelection, setReviewSelection] = useState<string[]>([]);
  const [reviewImportText, setReviewImportText] = useState('');
  const [reviewResult, setReviewResult] = useState<ReviewImportResult | null>(null);

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
    const item: Investigation = { batchId: analysis.batch.id, name, rootId: trace.record.id, traceId: trace.record.traceId, selectedId: selected?.record.id, notes, hypothesis: { name: hypothesisName, constraints }, view: { timeMode: 'bounds', expandedIds: [...expandedIds] } };
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
      setHypothesisName(reopened.investigation.hypothesis?.name || '');
      setConstraints(reopened.investigation.hypothesis?.constraints ?? []);
      setMessage('已重新打开调查，耗时由原始记录重新计算');
    } catch (error) { setMessage((error as Error).message); }
  }

  async function doClockAnalysis() {
    if (!analysis) { setMessage('请先导入批次'); return; }
    try {
      const result = await runClockAnalysis(analysis.batch.id, { name: hypothesisName, constraints });
      adoptAnalysis(result, traceId, selectedId, [...expandedIds]);
      const report = result.clock;
      if (report && !report.feasible) {
        setMessage(`约束分析：发现 ${report.conflicts?.length ?? 0} 组矛盾，假设不可行，已回退到仅设备时钟的区间`);
      } else {
        setMessage('约束分析完成：区间已按启用约束收紧（原始时间戳未改动）');
      }
    } catch (error) { setMessage((error as Error).message); }
  }

  function updateConstraint(id: string, patch: Partial<ClockConstraint>) {
    setConstraints(items => items.map(item => item.id === id ? { ...item, ...patch } : item));
  }

  async function doCompare() {
    if (!compareA || !compareB) { setMessage('请选择两次调查进行对照'); return; }
    try {
      setComparison(await compareInvestigations(compareA, compareB));
      setMessage('');
    } catch (error) { setComparison(null); setMessage((error as Error).message); }
  }

  async function doExportReview() {
    if (reviewSelection.length === 0) { setMessage('请选择一个或两个调查导出复核包'); return; }
    try {
      const pkg = await exportReview(reviewSelection);
      const blob = new Blob([JSON.stringify(pkg, null, 2)], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `review-${pkg.batch.id}.json`;
      link.click();
      URL.revokeObjectURL(url);
      setReviewResult(null);
      setMessage(`复核包已导出：${pkg.savedConclusions.length} 份封存结论，含启用/停用依据与冲突链`);
    } catch (error) { setMessage((error as Error).message); }
  }

  async function doImportReview() {
    if (!reviewImportText.trim()) { setMessage('请粘贴复核包 JSON 或选择文件'); return; }
    try {
      const pkg = JSON.parse(reviewImportText) as ReviewPackage;
      const result = await importReview(pkg);
      setReviewResult(result);
      setMessage(`复核包已导入：按包内原始依据重新求解${result.recomputed.conclusionMatch ? '，与原保存结论一致' : '，与原保存结论存在差异，详见复核结果'}`);
      setSaved(await listInvestigations());
    } catch (error) { setMessage((error as Error).message); }
  }

  function onReviewFile(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = () => setReviewImportText(String(reader.result || ''));
    reader.readAsText(file);
  }

  function toggleReviewSelection(id: string) {
    setReviewSelection(items => {
      if (items.includes(id)) return items.filter(item => item !== id);
      if (items.length >= 2) return [items[1], id];
      return [...items, id];
    });
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
        <ReviewPanel
          saved={saved}
          selected={reviewSelection}
          onToggle={toggleReviewSelection}
          onExport={doExportReview}
          importText={reviewImportText}
          onImportTextChange={setReviewImportText}
          onFile={onReviewFile}
          onImport={doImportReview}
          result={reviewResult}
        />
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
        {analysis && <HypothesisPanel
          analysis={analysis}
          hypothesisName={hypothesisName}
          onNameChange={setHypothesisName}
          constraints={constraints}
          onChange={updateConstraint}
          onAdd={() => setConstraints(items => [...items, newConstraint()])}
          onRemove={id => setConstraints(items => items.filter(item => item.id !== id))}
          onRun={doClockAnalysis}
        />}
        {analysis?.clock && <ClockReportView report={analysis.clock} />}
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

function HypothesisPanel({ analysis, hypothesisName, onNameChange, constraints, onChange, onAdd, onRemove, onRun }: {
  analysis: Analysis;
  hypothesisName: string;
  onNameChange: (value: string) => void;
  constraints: ClockConstraint[];
  onChange: (id: string, patch: Partial<ClockConstraint>) => void;
  onAdd: () => void;
  onRemove: (id: string) => void;
  onRun: () => void;
}) {
  const recordIds = (analysis.batch.records ?? []).map(record => record.id);
  return <div className="hypothesis">
    <h3>时钟假设与约束分析</h3>
    <p className="muted">依据只引用原始记录的开始/结束时刻，可写作 recordId、recordId:start 或 recordId:end；原始时间戳不会被修改。</p>
    <label>假设名称<input value={hypothesisName} onChange={e => onNameChange(e.target.value)} placeholder="例如：网关对时假设" /></label>
    <datalist id="record-ids">{recordIds.map(id => <option key={id} value={id} />)}</datalist>
    {constraints.length === 0 && <p className="muted">尚无约束。添加跨设备时差或先后顺序依据后运行约束分析。</p>}
    {constraints.map(item => <div key={item.id} className={`constraint ${item.disabled ? 'disabled' : ''}`}>
      <div className="constraint-row">
        <select aria-label="约束类型" value={item.type} onChange={e => onChange(item.id, { type: e.target.value })}>
          <option value="offset">时差范围</option>
          <option value="before">先后关系</option>
        </select>
        {item.type === 'offset' ? <>
          <input aria-label="基准记录" list="record-ids" placeholder="基准记录" value={item.anchor ?? ''} onChange={e => onChange(item.id, { anchor: e.target.value })} />
          <input aria-label="目标记录" list="record-ids" placeholder="目标记录" value={item.target ?? ''} onChange={e => onChange(item.id, { target: e.target.value })} />
        </> : <>
          <input aria-label="在先记录" list="record-ids" placeholder="在先记录" value={item.before ?? ''} onChange={e => onChange(item.id, { before: e.target.value })} />
          <input aria-label="在后记录" list="record-ids" placeholder="在后记录" value={item.after ?? ''} onChange={e => onChange(item.id, { after: e.target.value })} />
        </>}
        <input aria-label="下限毫秒" type="number" value={item.minMs} onChange={e => onChange(item.id, { minMs: Number(e.target.value) })} />
        <span className="muted">至</span>
        <input aria-label="上限毫秒" type="number" value={item.maxMs} onChange={e => onChange(item.id, { maxMs: Number(e.target.value) })} />
        <span className="muted">ms</span>
      </div>
      <div className="constraint-row">
        <input aria-label="依据说明" className="rationale" placeholder="依据说明（日志、对时方式）" value={item.rationale ?? ''} onChange={e => onChange(item.id, { rationale: e.target.value })} />
        <label className="toggle-enabled"><input type="checkbox" checked={!item.disabled} onChange={e => onChange(item.id, { disabled: !e.target.checked })} />启用</label>
        <button className="ghost" onClick={() => onRemove(item.id)}>删除</button>
      </div>
    </div>)}
    <div className="constraint-actions">
      <button className="ghost" onClick={onAdd}>添加约束</button>
      <button className="primary" onClick={onRun}>运行约束分析</button>
    </div>
  </div>;
}

function ClockReportView({ report }: { report: ClockReport }) {
  return <div className="clock-report">
    <h3>约束分析结果</h3>
    <p className={report.feasible ? 'feasible' : 'uncertain'}>
      {report.feasible ? '当前假设可行：所有启用约束可同时成立。' : '假设不可行：存在无法同时成立的约束，已回退到仅设备时钟的区间。'}
    </p>
    {(report.conflicts ?? []).map((conflict, index) => <div key={index} className="conflict">
      <b>{conflictLabel[conflict.type] || conflict.type}</b>
      <p>{conflict.summary}</p>
      {(conflict.constraintIds ?? []).length > 0 && <p>涉及约束：{conflict.constraintIds.join('、')}</p>}
      {(conflict.recordIds ?? []).length > 0 && <p>涉及记录：{conflict.recordIds.join('、')}</p>}
    </div>)}
    {(report.disabledConstraintIds ?? []).length > 0 && <p className="muted">已停用约束：{report.disabledConstraintIds!.join('、')}（未参与求解，可随时恢复）</p>}
    {(report.uncertainRecordIds ?? []).length > 0 && <p className="uncertain">仍无法确定相对顺序的记录：{report.uncertainRecordIds!.join('、')}（保留区间，不强行排名）</p>}
  </div>;
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
        {node.timing?.assumptionBased && <small className="assumption">区间已按时钟假设收紧（非原始证据直接证明）</small>}
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
    <dl>
      <dt>本段处理增量</dt><dd><DeltaValue value={comparison.executionDelta} /></dd>
      <dt>等待（含并行）增量</dt><dd><DeltaValue value={comparison.waitDelta} /></dd>
    </dl>
    <p className={comparison.evidenceIncomplete ? 'uncertain' : ''}>{attributionLabel[comparison.attribution] || comparison.attribution}</p>
    {comparison.assumptionChange && <small className="assumption">两次调查时钟假设不同：区间差异可能仅来自假设收紧，而非真实耗时变化</small>}
    {comparison.evidenceIncomplete && <small className="uncertain">证据不完整：存在异常记录或跨设备时钟不确定</small>}
    {(comparison.notes ?? []).map((note, index) => <small key={index} className="muted note">{note}</small>)}
  </div>;
}

function DeltaValue({ value }: { value: { nominal: number; lower?: number; upper?: number; certain: boolean } }) {
  if (!value.certain) return <span>{fmt(value.lower)}–{fmt(value.upper)} ms（名义 {fmt(value.nominal)}）</span>;
  return <span>{fmt(value.nominal)} ms</span>;
}

function AnomalyRow({ item }: { item: Anomaly }) { return <p className="anomaly"><b>{anomalyLabel[item.type] || item.type}</b><span>{item.summary}</span></p>; }

const reviewIssueLabel: Record<string, string> = {
  contradiction: '约束相互矛盾', invalid_constraint: '约束无效', unknown_record: '引用未知记录', invalid_range: '时差范围不成立',
  duplicate_id: '重复记录 ID', missing_record: '依据引用缺失记录', comparison_failed: '对照结论重算失败',
  batch_id_exists: '批次 ID 已存在', investigation_id_exists: '调查 ID 已存在'
};

function ReviewPanel({ saved, selected, onToggle, onExport, importText, onImportTextChange, onFile, onImport, result }: {
  saved: Investigation[];
  selected: string[];
  onToggle: (id: string) => void;
  onExport: () => void;
  importText: string;
  onImportTextChange: (value: string) => void;
  onFile: (event: ChangeEvent<HTMLInputElement>) => void;
  onImport: () => void;
  result: ReviewImportResult | null;
}) {
  return <div className="review">
    <h3>调查复核包</h3>
    <p className="muted">选择一个或两个调查导出：封存原始记录、启用/停用时钟依据、冲突链与对照结论；另一台同版本应用导入后按原依据重算。</p>
    {saved.map(item => <label key={item.id} className="review-pick">
      <input type="checkbox" checked={selected.includes(item.id || '')} onChange={() => item.id && onToggle(item.id)} />
      <span>{item.name}</span>
    </label>)}
    <button onClick={onExport} disabled={selected.length === 0}>导出复核包</button>
    <label className="review-file">导入复核包<input type="file" accept="application/json,.json" onChange={onFile} /></label>
    <textarea value={importText} onChange={event => onImportTextChange(event.target.value)} placeholder="或直接粘贴复核包 JSON" spellCheck={false} />
    <button className="ghost" onClick={onImport} disabled={!importText.trim()}>导入并重算</button>
    {result && <ReviewResultView result={result} />}
  </div>;
}

function ReviewResultView({ result }: { result: ReviewImportResult }) {
  const { recomputed, issues, package: pkg } = result;
  return <div className="review-result">
    <h4>复核结果</h4>
    <p className={recomputed.conclusionMatch ? 'feasible' : 'uncertain'}>
      {recomputed.conclusionMatch ? '重新求解结果与原保存结论一致。' : '重新求解结果与原保存结论不一致：原保存结论与重算结果已分开列出。'}
    </p>
    {recomputed.comparisonMatch !== undefined && <p className={recomputed.comparisonMatch ? 'feasible' : 'uncertain'}>
      调查对照结论重算{recomputed.comparisonMatch ? '一致' : '不一致'}（{pkg.investigations[0]?.name} → {pkg.investigations[1]?.name}）。
    </p>}
    {pkg.savedConclusions.map(saved => {
      const entry = recomputed.entries.find(item => item.investigationId === saved.investigationId);
      return <div key={saved.investigationId} className="review-conclusion">
        <b>{saved.name}</b>
        <p>原保存结论：{saved.clock.feasible ? '可行' : '不可行'}，约束 {saved.clock.constraintIds?.length ?? 0} 条，停用 {saved.clock.disabledConstraintIds?.length ?? 0} 条，冲突链 {saved.clock.conflicts?.length ?? 0} 组</p>
        <p>重新求解：{entry?.clock.feasible ? '可行' : '不可行'}，约束 {entry?.clock.constraintIds?.length ?? 0} 条，停用 {entry?.clock.disabledConstraintIds?.length ?? 0} 条，冲突链 {entry?.clock.conflicts?.length ?? 0} 组</p>
        {saved.clock.conflicts?.map((conflict, i) => <small key={i} className="uncertain">封存冲突：{conflict.constraintIds.join('、')}（{conflict.recordIds.join('、')}）</small>)}
      </div>;
    })}
    {(recomputed.notes ?? []).map((note, i) => <small key={i} className="muted note">{note}</small>)}
    {issues.length > 0 && <div className="review-issues">
      <p className="uncertain">问题清单（{issues.length} 项，依据均已保留）：</p>
      {issues.map((issue, i) => <p key={i} className={issue.severity === 'error' ? 'anomaly' : 'muted'}>
        <b>[{issue.severity === 'error' ? '错误' : '警告'}] {reviewIssueLabel[issue.type] || issue.type}</b>
        <span>{issue.summary}</span>
        {issue.constraintIds?.length ? <span>约束：{issue.constraintIds.join('、')}</span> : null}
        {issue.recordIds?.length ? <span>记录：{issue.recordIds.join('、')}</span> : null}
      </p>)}
    </div>}
  </div>;
}
function kids(node: NodeType): NodeType[] { return node.children ?? []; }
function flatten(node: NodeType): NodeType[] { return [node, ...kids(node).flatMap(flatten)]; }
function findNode(node: NodeType, id: string): NodeType | null { if (node.record.id === id) return node; for (const child of kids(node)) { const found = findNode(child, id); if (found) return found; } return null; }
function fmt(value: number | undefined) { return Number.isFinite(value) ? Math.round((value || 0) * 100) / 100 : '—'; }
