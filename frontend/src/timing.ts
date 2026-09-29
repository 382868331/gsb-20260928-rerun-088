// Interval union: the portion of a parent window covered by at least one
// child. Parallel children are counted once, never summed.
export function unionLength(intervals: Array<[number, number]>): number {
  if (intervals.length === 0) return 0;
  const sorted = [...intervals].sort((a, b) => a[0] - b[0] || a[1] - b[1]);
  let total = 0;
  let [curStart, curEnd] = sorted[0];
  for (const [s, e] of sorted.slice(1)) {
    if (s <= curEnd) {
      if (e > curEnd) curEnd = e;
    } else {
      total += Math.max(0, curEnd - curStart);
      [curStart, curEnd] = [s, e];
    }
  }
  return total + Math.max(0, curEnd - curStart);
}

// Breakdown mirroring the Go analysis so the UI can show the same numbers
// when it needs them locally (server values remain authoritative).
export interface BreakdownInput {
  startNs: number;
  endNs: number;
  children: Array<{ startNs: number; endNs: number }>;
  toleranceNs?: number;
}

export function breakdown(input: BreakdownInput) {
  const { startNs, endNs } = input;
  const tolerance = input.toleranceNs ?? 1_000_000;
  const duration = endNs - startNs;
  const clipped: Array<[number, number]> = [];
  let outside = 0;
  let clockUncertain = false;
  for (const c of input.children) {
    const childLen = Math.max(0, c.endNs - c.startNs);
    const cs = Math.max(c.startNs, startNs);
    const ce = Math.min(c.endNs, endNs);
    const inLen = Math.max(0, ce - cs);
    if (inLen > 0) clipped.push([cs, ce]);
    const out = childLen - inLen;
    if (out > tolerance) {
      clockUncertain = true;
      outside += out - tolerance;
    }
  }
  const waiting = unionLength(clipped);
  return {
    durationNs: duration,
    waitingNs: waiting,
    executingNs: duration > 0 ? Math.max(0, duration - waiting) : 0,
    overlapNs: clipped.reduce((n, iv) => n + (iv[1] - iv[0]), 0),
    outsideNs: outside,
    clockUncertain,
  };
}
