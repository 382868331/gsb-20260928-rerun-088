export function fmtNs(ns: number): string {
  if (ns === 0) return "0";
  const abs = Math.abs(ns);
  if (abs < 1_000) return `${ns}ns`;
  if (abs < 1_000_000) return `${(ns / 1_000).toFixed(2)}µs`;
  if (abs < 1_000_000_000) return `${(ns / 1_000_000).toFixed(2)}ms`;
  return `${(ns / 1_000_000_000).toFixed(3)}s`;
}

export function fmtClock(ns: number): string {
  if (!ns) return "-";
  return new Date(ns / 1e6).toISOString().replace("T", " ").replace("Z", " UTC");
}
