import type { Investigation } from "../types";

export function SavedList({ saved, onOpen }: { saved: Investigation[]; onOpen: (i: Investigation) => void }) {
  return (
    <section className="panel">
      <h2>已保存调查（{saved.length}）</h2>
      <ul className="savelist">
        {saved.map((i) => (
          <li key={i.id}>
            <button className="linkbtn" onClick={() => onOpen(i)}>
              {i.title || i.traceId}
            </button>
            <span className="muted small mono">{i.traceId}</span>
          </li>
        ))}
      </ul>
      <p className="hint small">重新打开会用原始证据重新复算耗时拆分，不依赖保存时的派生数字。</p>
    </section>
  );
}
