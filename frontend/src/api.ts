import type { Batch, Investigation, Trace, TraceSummary } from "./types";

async function jsonFetch<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, init);
  if (!res.ok) {
    let msg = `${res.status}`;
    try {
      const body = await res.json();
      msg = body.error || msg;
    } catch {
      /* ignore */
    }
    throw new Error(msg);
  }
  return res.json();
}

export const api = {
  async listBatches(): Promise<Batch[]> {
    const r = await jsonFetch<{ batches: Batch[] }>("/api/batches");
    return r.batches;
  },
  async importBatch(format: string, filename: string, content: string): Promise<Batch> {
    const r = await jsonFetch<{ batch: Batch }>("/api/batches", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ format, filename, content }),
    });
    return r.batch;
  },
  async listTraces(): Promise<TraceSummary[]> {
    const r = await jsonFetch<{ traces: TraceSummary[] }>("/api/traces");
    return r.traces;
  },
  async getTrace(traceId: string): Promise<Trace> {
    const r = await jsonFetch<{ trace: Trace }>(`/api/traces/${encodeURIComponent(traceId)}`);
    return r.trace;
  },
  async listInvestigations(): Promise<Investigation[]> {
    const r = await jsonFetch<{ investigations: Investigation[] }>("/api/investigations");
    return r.investigations;
  },
  async saveInvestigation(inv: Investigation): Promise<{ investigation: Investigation; trace: Trace }> {
    return jsonFetch("/api/investigations", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(inv),
    });
  },
  async openInvestigation(id: string): Promise<{ investigation: Investigation; trace: Trace }> {
    return jsonFetch(`/api/investigations/${encodeURIComponent(id)}`);
  },
};
