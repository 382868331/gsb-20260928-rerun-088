import { useState } from "react";
import { api } from "../api";
import type { Batch } from "../types";
import { fmtClock } from "../format";

const SAMPLE = [
  '{"id":"r","traceId":"checkout-7","name":"POST /checkout","service":"api","device":"host-a","start":"2026-09-29T10:00:00Z","end":"2026-09-29T10:00:10Z"}',
  '{"id":"a","parentId":"r","traceId":"checkout-7","name":"db.stock","service":"db","device":"host-b","start":"2026-09-29T10:00:01Z","end":"2026-09-29T10:00:07Z"}',
  '{"id":"b","parentId":"r","traceId":"checkout-7","name":"http.charge","service":"pay","device":"host-c","start":"2026-09-29T10:00:02Z","end":"2026-09-29T10:00:08Z"}',
  '{"id":"c","parentId":"a","traceId":"checkout-7","name":"db.conn-pool","service":"db","device":"host-b","start":"2026-09-29T10:00:02Z","end":"2026-09-29T10:00:03Z"}',
  '{"id":"x","parentId":"missing-svc","traceId":"checkout-7","name":"orphan.audit","service":"audit","device":"host-d","start":"2026-09-29T10:00:00Z","end":"2026-09-29T10:00:02Z"}',
  '{"id":"r","traceId":"checkout-7","name":"POST /checkout","service":"api","device":"host-a","start":"2026-09-29T10:00:00Z","end":"2026-09-29T10:00:11Z"}',
].join("\n");

export function ImportPanel({ batches, onImported }: { batches: Batch[]; onImported: () => void }) {
  const [content, setContent] = useState("");
  const [filename, setFilename] = useState("manual.jsonl");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  async function doImport() {
    setBusy(true);
    setErr("");
    try {
      await api.importBatch("manual-jsonl", filename, content);
      setContent("");
      onImported();
    } catch (e) {
      setErr(String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="panel">
      <h2>来源批次</h2>
      <textarea
        value={content}
        onChange={(e) => setContent(e.target.value)}
        placeholder="粘贴人工调用记录（JSONL，每行一条；支持 RFC3339 / 毫秒 / 微秒 / 纳秒时间戳）"
        rows={8}
      />
      <input value={filename} onChange={(e) => setFilename(e.target.value)} placeholder="文件名" />
      <div className="row">
        <button disabled={busy || !content.trim()} onClick={doImport}>
          {busy ? "导入中…" : "导入批次"}
        </button>
        <button className="ghost" onClick={() => setContent(SAMPLE)}>
          载入示例
        </button>
      </div>
      {err && <div className="error">{err}</div>}
      <ul className="batchlist">
        {batches.map((b) => (
          <li key={b.id} title={b.id}>
            <span className="mono">{b.filename || b.id}</span>
            <span className="muted">{b.format} · {b.spanCount} 条 · {fmtClock(new Date(b.importedAt).getTime() * 1e6)}</span>
          </li>
        ))}
      </ul>
    </section>
  );
}
