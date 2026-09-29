import { useMemo, useState } from "react";
import { api } from "../api";
import type { Trace, TraceSummary, TreeNode } from "../types";
import { fmtNs, fmtClock } from "../format";

const FLAG_LABEL: Record<string, string> = {
  missing_parent: "缺失父调用",
  duplicate: "重复记录",
  clock_anomaly: "时钟异常",
};

function collect(n: TreeNode, acc: TreeNode[] = []): TreeNode[] {
  acc.push(n);
  n.children.forEach((c) => collect(c, acc));
  return acc;
}

export function TraceView({
  trace,
  summary,
  onSaved,
}: {
  trace: Trace;
  summary: TraceSummary;
  onSaved: () => void;
}) {
  const [selectedNodeId, setSelectedNodeId] = useState<string>(trace.nodes[0]?.span.id ?? "");
  const [title, setTitle] = useState(`${summary.rootName} 慢请求调查`);
  const [notes, setNotes] = useState("");
  const [saveMsg, setSaveMsg] = useState("");
  const [saving, setSaving] = useState(false);

  const flat = useMemo(() => {
    const all: TreeNode[] = [];
    trace.roots.forEach((r) => collect(r, all));
    return all;
  }, [trace]);

  const bounds = useMemo(() => {
    let min = Infinity;
    let max = -Infinity;
    flat.forEach((n) => {
      min = Math.min(min, n.span.startNs);
      max = Math.max(max, n.span.endNs);
    });
    if (!isFinite(min)) return { min: 0, max: 1, width: 1 };
    // biased children can extend far beyond the root; keep the full raw
    // extent visible rather than snapping them back.
    return { min, max, width: Math.max(1, max - min) };
  }, [flat]);

  const selectedNode = flat.find((n) => n.span.id === selectedNodeId && n.depth === trace.nodes.find((x) => x.span.id === selectedNodeId)?.depth) ?? flat[0];

  async function save() {
    setSaving(true);
    setSaveMsg("");
    try {
      await api.saveInvestigation({
        title,
        notes,
        traceId: trace.traceId,
        batchIds: summary.batchIds,
        rootSpanId: trace.rootSpanId,
      });
      setSaveMsg("已保存，重新打开时将从原始记录复算。");
      onSaved();
    } catch (e) {
      setSaveMsg(String(e));
    } finally {
      setSaving(false);
    }
  }

  const rb = trace.rootSummary;

  return (
    <section className="panel traceview">
      <h2>
        {summary.rootName} <span className="muted mono small">{trace.traceId}</span>
      </h2>

      {rb && (
        <div className="summarycards">
          <Card label="总耗时" value={fmtNs(rb.durationNs)} />
          <Card label="实际执行（未被子调用覆盖）" value={fmtNs(rb.executingNs)} kind="exec" pct={pct(rb.executingNs, rb.durationNs)} />
          <Card label="等待子调用（区间并集）" value={fmtNs(rb.waitingNs)} kind="wait" pct={pct(rb.waitingNs, rb.durationNs)} />
          <Card label="并行重叠（重复计入量）" value={fmtNs(rb.overlapNs - rb.waitingNs)} kind="overlap" />
          {rb.outsideNs > 0 && <Card label="超出窗口（疑似时钟偏差）" value={fmtNs(rb.outsideNs)} kind="outside" />}
        </div>
      )}
      {rb?.clockUncertain && (
        <div className="warn">存在跨设备时钟偏差：仅统计能确定落在父窗口内的部分，不强行对齐时间。</div>
      )}

      <div className="split">
        <div className="tree">
          <div className="rowheader">调用树 / 瀑布（原始时间轴）</div>
          {flat.map((n) => {
            const s = n.span;
            const left = ((s.startNs - bounds.min) / bounds.width) * 100;
            const width = Math.max(0.5, ((s.endNs - s.startNs) / bounds.width) * 100);
            const b = n.breakdown;
            const active = s.id === selectedNode?.span.id;
            return (
              <div key={`${s.id}-${s.seq}`} className={`treerow ${active ? "active" : ""}`} onClick={() => setSelectedNodeId(s.id)}>
                <div className="treename" style={{ paddingLeft: 8 + n.depth * 16 }}>
                  {n.flags?.map((f) => (
                    <span key={f} className={`badge badge-${f}`}>{FLAG_LABEL[f] ?? f}</span>
                  ))}
                  <span className="op">{s.name}</span>
                  <span className="muted small">{s.service}{s.device ? ` · ${s.device}` : ""}</span>
                </div>
                <div className="waterfall" title={`${fmtNs(b.executingNs)} 执行 / ${fmtNs(b.waitingNs)} 等待`}>
                  <div className="bar-track">
                    <div className="bar-exec" style={{ left: `${left}%`, width: `${width}%` }}>
                      <div
                        className="bar-wait"
                        style={{ width: b.durationNs > 0 ? `${(b.waitingNs / b.durationNs) * 100}%` : 0 }}
                      />
                    </div>
                  </div>
                  <span className="dur mono">{fmtNs(s.endNs - s.startNs)}</span>
                </div>
              </div>
            );
          })}
          <Legend />
        </div>

        <div className="evidence">
          <div className="rowheader">原始证据</div>
          {selectedNode && <NodeEvidence node={selectedNode} trace={trace} />}
        </div>
      </div>

      {trace.notes && trace.notes.length > 0 && (
        <ul className="notes">
          {trace.notes.map((n, i) => (
            <li key={i}>{n}</li>
          ))}
        </ul>
      )}

      <div className="savebox">
        <input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="调查标题" />
        <textarea value={notes} onChange={(e) => setNotes(e.target.value)} placeholder="调查结论（耗时主要花在等待下游还是自身执行…）" rows={2} />
        <div className="row">
          <button disabled={saving} onClick={save}>{saving ? "保存中…" : "保存调查视图"}</button>
          {saveMsg && <span className="muted small">{saveMsg}</span>}
        </div>
      </div>
    </section>
  );
}

function pct(a: number, b: number) {
  return b > 0 ? Math.round((a / b) * 100) : 0;
}

function Card({ label, value, kind, pct: p }: { label: string; value: string; kind?: "exec" | "wait" | "overlap" | "outside"; pct?: number }) {
  return (
    <div className={`card card-${kind ?? ""}`}>
      <div className="cardlabel">{label}{p !== undefined && ` · ${p}%`}</div>
      <div className="cardvalue">{value}</div>
    </div>
  );
}

function Legend() {
  return (
    <div className="legend">
      <span><i className="sw sw-exec" /> 实际执行</span>
      <span><i className="sw sw-wait" /> 等待子调用（并集，不重复相加）</span>
    </div>
  );
}

function NodeEvidence({ node, trace }: { node: TreeNode; trace: Trace }) {
  const s = node.span;
  const b = node.breakdown;
  return (
    <div>
      <h3 className="evtitle">{s.name} <span className="muted mono small">{s.id}</span></h3>
      {node.flags?.map((f) => (
        <span key={f} className={`badge badge-${f}`}>{FLAG_LABEL[f] ?? f}</span>
      ))}
      <dl className="kv">
        <dt>服务 / 设备</dt><dd>{s.service || "-"}{s.device ? ` / ${s.device}` : ""}</dd>
        <dt>父调用</dt><dd className="mono">{s.parentId || "（无，根调用）"}{node.flags?.includes("missing_parent") ? ` — 未在任何批次中找到` : ""}</dd>
        <dt>来源批次</dt><dd className="mono">{s.source} · 文件第 {s.seq} 行</dd>
        <dt>开始（原始时钟）</dt><dd className="mono">{fmtClock(s.startNs)}</dd>
        <dt>结束（原始时钟）</dt><dd className="mono">{fmtClock(s.endNs)}</dd>
        <dt>耗时</dt><dd className="mono">{fmtNs(b.durationNs)}</dd>
        <dt>实际执行</dt><dd className="mono">{fmtNs(b.executingNs)}</dd>
        <dt>等待子调用</dt><dd className="mono">{fmtNs(b.waitingNs)}（子区间并集）</dd>
        <dt>子调用原始求和</dt><dd className="mono">{fmtNs(b.overlapNs)}（并行部分会大于并集）</dd>
        <dt>超出父窗口</dt><dd className="mono">{b.outsideNs > 0 ? fmtNs(b.outsideNs) : "-"}{b.clockUncertain ? " · 时钟不确定" : ""}</dd>
      </dl>
      {node.duplicates && node.duplicates.length > 0 && (
        <details open>
          <summary>重复记录（{node.duplicates.length} 条，保留为证据，未参与建树）</summary>
          {node.duplicates.map((d, i) => (
            <pre key={i} className="raw">{JSON.stringify(d.raw ?? d, null, 2)}</pre>
          ))}
        </details>
      )}
      <details open>
        <summary>原始记录（逐字保存）</summary>
        <pre className="raw">{JSON.stringify(s.raw ?? s, null, 2)}</pre>
      </details>
      <p className="muted small">trace: {trace.traceId} · 时间戳不做跨设备改写。</p>
    </div>
  );
}
