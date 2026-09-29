import type { TraceSummary } from "../types";
import { fmtNs } from "../format";

const FLAG_LABEL: Record<string, string> = {
  missing_parent: "缺失父调用",
  duplicate: "重复记录",
  clock_anomaly: "时钟异常",
};

export function TraceList({
  traces,
  selectedId,
  onSelect,
}: {
  traces: TraceSummary[];
  selectedId?: string;
  onSelect: (t: TraceSummary) => void;
}) {
  return (
    <section className="panel">
      <h2>慢请求（{traces.length}）</h2>
      <table className="grid">
        <thead>
          <tr>
            <th>请求</th>
            <th>服务</th>
            <th>总耗时</th>
            <th>调用数</th>
            <th>标记</th>
          </tr>
        </thead>
        <tbody>
          {traces.map((t) => (
            <tr
              key={t.traceId}
              className={t.traceId === selectedId ? "selected" : ""}
              onClick={() => onSelect(t)}
            >
              <td className="mono">{t.rootName}<div className="muted small">{t.traceId}</div></td>
              <td>{t.service || "-"}</td>
              <td className="num strong">{fmtNs(t.durationNs)}</td>
              <td className="num">{t.spanCount}</td>
              <td>
                {t.flags.map((f) => (
                  <span key={f} className={`badge badge-${f}`}>{FLAG_LABEL[f] ?? f}</span>
                ))}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {traces.length === 0 && <p className="hint">还没有批次。导入人工调用记录后，这里会按请求列出耗时。</p>}
    </section>
  );
}
