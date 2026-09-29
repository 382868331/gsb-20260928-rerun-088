import { describe, expect, it } from "vitest";
import { breakdown, unionLength } from "../timing";

describe("unionLength", () => {
  it("merges parallel overlap instead of summing", () => {
    expect(unionLength([[2, 8], [3, 9]])).toBe(7);
    expect(unionLength([[1, 4], [4, 8]])).toBe(7);
    expect(unionLength([])).toBe(0);
  });
});

describe("breakdown", () => {
  it("splits waiting vs executing with parallel children", () => {
    const b = breakdown({
      startNs: 0, endNs: 10_000,
      children: [{ startNs: 1_000, endNs: 7_000 }, { startNs: 2_000, endNs: 8_000 }],
    });
    expect(b.waitingNs).toBe(7_000);
    expect(b.executingNs).toBe(3_000);
    expect(b.overlapNs).toBe(12_000); // raw sum: parallel double-count visible
  });

  it("keeps biased clocks outside instead of forcing them in", () => {
    const b = breakdown({
      startNs: 0, endNs: 10_000_000_000,
      children: [{ startNs: 25_000_000_000, endNs: 27_000_000_000 }],
    });
    expect(b.waitingNs).toBe(0);
    expect(b.executingNs).toBe(10_000_000_000);
    expect(b.clockUncertain).toBe(true);
    expect(b.outsideNs).toBeGreaterThan(1_000_000);
  });
});
