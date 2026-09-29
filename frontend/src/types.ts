export interface Breakdown {
  executingNs: number;
  waitingNs: number;
  overlapNs: number;
  outsideNs: number;
  unknownNs: number;
  durationNs: number;
  recomputed: boolean;
  clockUncertain: boolean;
}

export type NodeFlag = "missing_parent" | "duplicate" | "clock_anomaly";

export interface Span {
  id: string;
  parentId?: string;
  traceId?: string;
  service?: string;
  name: string;
  kind?: string;
  startNs: number;
  endNs: number;
  device?: string;
  source: string;
  seq: number;
  raw?: unknown;
}

export interface SpanDup extends Span {}

export interface TreeNode {
  span: Span;
  depth: number;
  children: TreeNode[];
  flags?: NodeFlag[];
  breakdown: Breakdown;
  duplicates?: SpanDup[];
}

export interface Trace {
  traceId: string;
  roots: TreeNode[];
  nodes: TreeNode[];
  rootSpanId: string;
  notes?: string[];
  rootSummary?: Breakdown;
}

export interface Batch {
  id: string;
  format: string;
  filename?: string;
  importedAt: string;
  spanCount: number;
  note?: string;
}

export interface TraceSummary {
  traceId: string;
  rootName: string;
  service: string;
  startNs: number;
  durationNs: number;
  spanCount: number;
  batchIds: string[];
  flags: string[];
}

export interface Investigation {
  id?: string;
  title: string;
  traceId: string;
  batchIds: string[];
  rootSpanId?: string;
  notes?: string;
  tags?: string[];
  createdAt?: string;
  updatedAt?: string;
}
