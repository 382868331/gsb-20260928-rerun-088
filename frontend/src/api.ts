import type { Analysis, ClockHypothesis, Comparison, Investigation, ReviewImportResult } from './types';

export async function importManual(payload: string): Promise<Analysis> {
  const response = await fetch('/api/import/manual', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: payload });
  if (!response.ok) throw new Error(await errorText(response));
  return response.json();
}

export async function getAnalysis(batchId: string): Promise<Analysis> {
  const response = await fetch(`/api/batches/${batchId}/analysis`);
  if (!response.ok) throw new Error(await errorText(response));
  return response.json();
}

export async function runClockAnalysis(batchId: string, hypothesis: ClockHypothesis): Promise<Analysis> {
  const response = await fetch(`/api/batches/${batchId}/clock-analysis`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(hypothesis) });
  if (!response.ok) throw new Error(await errorText(response));
  return response.json();
}

export async function listInvestigations(): Promise<Investigation[]> {
  const response = await fetch('/api/investigations');
  return response.ok ? response.json() : [];
}

export async function saveInvestigation(item: Investigation): Promise<Investigation> {
  const response = await fetch('/api/investigations', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(item) });
  if (!response.ok) throw new Error(await errorText(response));
  return response.json();
}

export async function openInvestigation(id: string): Promise<{ investigation: Investigation; analysis: Analysis }> {
  const response = await fetch(`/api/investigations/${id}`);
  if (!response.ok) throw new Error(await errorText(response));
  return response.json();
}

export async function compareInvestigations(a: string, b: string): Promise<Comparison> {
  const response = await fetch(`/api/investigations/compare?a=${encodeURIComponent(a)}&b=${encodeURIComponent(b)}`);
  if (!response.ok) throw new Error(await errorText(response));
  return response.json();
}

export async function exportReviewBundle(id: string, compareWith?: string): Promise<unknown> {
  const query = compareWith ? `?compareWith=${encodeURIComponent(compareWith)}` : '';
  const response = await fetch(`/api/investigations/${encodeURIComponent(id)}/review-bundle${query}`);
  if (!response.ok) throw new Error(await errorText(response));
  return response.json();
}

export async function importReviewBundle(payload: string): Promise<ReviewImportResult> {
  const response = await fetch('/api/review-bundles/import', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: payload });
  if (!response.ok) throw new Error(await errorText(response));
  return response.json();
}

async function errorText(response: Response) { try { const body = await response.json(); return body.error || response.statusText; } catch { return response.statusText; } }
