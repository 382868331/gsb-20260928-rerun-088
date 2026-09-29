import { useCallback, useEffect, useState } from "react";
import { api } from "./api";
import type { Batch, Investigation, Trace, TraceSummary } from "./types";
import { ImportPanel } from "./components/ImportPanel";
import { TraceList } from "./components/TraceList";
import { TraceView } from "./components/TraceView";
import { SavedList } from "./components/SavedList";

export function App() {
  const [batches, setBatches] = useState<Batch[]>([]);
  const [traces, setTraces] = useState<TraceSummary[]>([]);
  const [saved, setSaved] = useState<Investigation[]>([]);
  const [selected, setSelected] = useState<TraceSummary | null>(null);
  const [trace, setTrace] = useState<Trace | null>(null);
  const [error, setError] = useState<string>("");
  const [loading, setLoading] = useState(false);

  const refreshAll = useCallback(async () => {
    setError("");
    try {
      const [b, t, i] = await Promise.all([
        api.listBatches(),
        api.listTraces(),
        api.listInvestigations(),
      ]);
      setBatches(b);
      setTraces(t);
      setSaved(i);
    } catch (e) {
      setError(String(e));
    }
  }, []);

  useEffect(() => {
    void refreshAll();
  }, [refreshAll]);

  const openTrace = useCallback(async (s: TraceSummary) => {
    setLoading(true);
    setSelected(s);
    setTrace(null);
    try {
      setTrace(await api.getTrace(s.traceId));
    } catch (e) {
      setError(String(e));
    } finally {
      setLoading(false);
    }
  }, []);

  const openSaved = useCallback(async (inv: Investigation) => {
    setLoading(true);
    setError("");
    try {
      const r = await api.openInvestigation(inv.id!);
      const sum: TraceSummary = {
        traceId: r.trace.traceId,
        rootName: r.trace.nodes[0]?.span.name ?? "",
        service: r.trace.nodes[0]?.span.service ?? "",
        startNs: r.trace.nodes[0]?.span.startNs ?? 0,
        durationNs: r.trace.rootSummary?.durationNs ?? 0,
        spanCount: r.trace.nodes.length,
        batchIds: inv.batchIds,
        flags: [],
      };
      setSelected(sum);
      setTrace(r.trace);
    } catch (e) {
      setError(String(e));
    } finally {
      setLoading(false);
    }
  }, []);

  return (
    <div className="app">
      <header className="topbar">
        <h1>服务性能调查平台</h1>
        <span className="subtitle">人工调用记录 · 因果还原 · 耗时拆分（等待 / 实际执行）</span>
      </header>
      {error && <div className="errorbar">{error}</div>}
      <div className="layout">
        <aside className="sidebar">
          <ImportPanel batches={batches} onImported={refreshAll} />
          <SavedList saved={saved} onOpen={openSaved} />
        </aside>
        <main className="main">
          <TraceList traces={traces} selectedId={selected?.traceId} onSelect={openTrace} />
          {loading && <div className="hint">正在从原始记录复算…</div>}
          {trace && selected && (
            <TraceView
              trace={trace}
              summary={selected}
              onSaved={refreshAll}
            />
          )}
        </main>
      </div>
    </div>
  );
}
