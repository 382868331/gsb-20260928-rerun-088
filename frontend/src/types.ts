export type Breakdown = { nominal: number; lower?: number; upper?: number; certain: boolean; known?: boolean; reason?: string };
export type CallRecord = {
  id: string; traceId: string; parentId?: string; service: string; operation: string;
  deviceId: string; start: string; end: string; durationMs: Breakdown;
  attributes?: Record<string, unknown>; raw?: Record<string, unknown>; anomalies?: string[]; duplicate?: boolean;
};
export type Timing = { durationMs: number; startMs?: Breakdown; endMs?: Breakdown; waitMs: Breakdown; executionMs: Breakdown; boundsCertain: boolean; parallel: boolean; reason?: string };
export type Node = { record: CallRecord; children: NodeType[]; timing: Timing; level: number };
export type RecordType = CallRecord;
export type NodeType = Node;
export type Summary = { rootId: string; traceId: string; service: string; operation: string; start: string; durationMs: number; waitMs: number; executionMs: number; spanCount: number; anomalyCount: number; certain: boolean };
export type Anomaly = { type: string; recordId?: string; recordIds?: string[]; constraintIds?: string[]; traceId?: string; summary: string };
export type Analysis = { batch: { id: string; source: string; format: string; importedAt: string; description?: string; records: RecordType[] }; requests: Summary[]; traces: Record<string, NodeType>; anomalies: Anomaly[]; generatedAt: string };
export type ClockConstraint = { id: string; type: 'order' | 'offset_range'; aRecordId: string; bRecordId: string; lowerMs?: number; upperMs?: number; note?: string };
export type Investigation = { id?: string; batchId: string; name: string; rootId: string; traceId: string; selectedId?: string; notes?: string; assumptions?: ClockConstraint[]; view?: { showAnomaliesOnly?: boolean; expandedIds?: string[]; timeMode?: string } };
export type ComparisonSide = { investigationId: string; name: string; rootId: string; traceId: string; durationMs: number; executionMs: Breakdown; waitMs: Breakdown; certain: boolean; parallel: boolean; anomalyCount: number };
export type RequestComparison = { traceId: string; rootId: string; service: string; operation: string; durationA: Breakdown; durationB: Breakdown; waitShareA: Breakdown; waitShareB: Breakdown; orderUndecidable: boolean };
export type Comparison = { a: ComparisonSide; b: ComparisonSide; durationDeltaMs: number; executionDelta: Breakdown; waitDelta: Breakdown; attribution: string; assumptionChange?: string; requests?: RequestComparison[]; rankingNotes?: string[]; evidenceIncomplete: boolean; notes: string[] };
