export type Breakdown = { nominal: number; lower?: number; upper?: number; certain: boolean; reason?: string };
export type CallRecord = {
  id: string; traceId: string; parentId?: string; service: string; operation: string;
  deviceId: string; start: string; end: string; durationMs: Breakdown;
  attributes?: Record<string, unknown>; raw?: Record<string, unknown>; anomalies?: string[]; duplicate?: boolean;
};
export type Timing = { durationMs: number; waitMs: Breakdown; executionMs: Breakdown; boundsCertain: boolean; assumptionBased?: boolean; parallel: boolean; reason?: string };
export type Node = { record: CallRecord; children: NodeType[]; timing: Timing; level: number };
export type RecordType = CallRecord;
export type NodeType = Node;
export type Summary = { rootId: string; traceId: string; service: string; operation: string; start: string; durationMs: number; waitMs: number; executionMs: number; spanCount: number; anomalyCount: number; certain: boolean };
export type Anomaly = { type: string; recordId?: string; traceId?: string; summary: string };
export type ClockConstraint = { id: string; type: string; before?: string; after?: string; anchor?: string; target?: string; minMs: number; maxMs: number; rationale?: string; disabled?: boolean };
export type ClockConflict = { type: string; constraintIds: string[]; recordIds: string[]; summary: string };
export type ClockReport = { feasible: boolean; conflicts?: ClockConflict[]; constraintIds?: string[]; disabledConstraintIds?: string[]; uncertainRecordIds?: string[] };
export type ClockHypothesis = { name?: string; constraints?: ClockConstraint[] };
export type Analysis = { batch: { id: string; source: string; format: string; importedAt: string; description?: string; records: RecordType[] }; requests: Summary[]; traces: Record<string, NodeType>; anomalies: Anomaly[]; clock?: ClockReport; generatedAt: string };
export type Investigation = { id?: string; batchId: string; name: string; rootId: string; traceId: string; selectedId?: string; notes?: string; hypothesis?: ClockHypothesis; view?: { showAnomaliesOnly?: boolean; expandedIds?: string[]; timeMode?: string } };
export type ComparisonSide = { investigationId: string; name: string; rootId: string; traceId: string; durationMs: number; executionMs: Breakdown; waitMs: Breakdown; certain: boolean; parallel: boolean; anomalyCount: number };
export type Comparison = { a: ComparisonSide; b: ComparisonSide; durationDeltaMs: number; executionDelta: Breakdown; waitDelta: Breakdown; attribution: string; evidenceIncomplete: boolean; assumptionChange?: boolean; notes: string[] };
export type ReviewIssue = { severity: 'error' | 'warning'; type: string; constraintIds?: string[]; recordIds?: string[]; investigationId?: string; summary: string };
export type SavedConclusion = { investigationId: string; name: string; rootId: string; traceId: string; clock: ClockReport; notes?: string; savedAt: string };
export type RecomputedEntry = { investigationId: string; found: boolean; clock: ClockReport; rootTiming?: Timing };
export type RecomputeSummary = { entries: RecomputedEntry[]; conclusionMatch: boolean; comparisonMatch?: boolean; notes?: string[] };
export type ReviewPackage = {
  format: string; exportedAt: string;
  batch: Analysis['batch']; investigations: Investigation[]; savedConclusions: SavedConclusion[]; comparison?: Comparison;
};
export type ReviewImportResult = { package: ReviewPackage; recomputed: RecomputeSummary; issues: ReviewIssue[] };
