package ingest

import (
	"strings"
	"testing"
	"time"
)

func TestManualJSONLVariants(t *testing.T) {
	in := strings.Join([]string{
		`# comment line`,
		`{"id":"r","traceId":"t","name":"GET /x","service":"api","start":"2026-09-29T10:00:00Z","end":"2026-09-29T10:00:02Z","device":"host-a"}`,
		`{"spanId":"c","parent_id":"r","trace_id":"t","name":"db","startMs":1790704800000,"endMs":1790704801500}`,
		`{"id":"d","parentId":"r","traceId":"t","operation":"cache","startNs":1790704800000000000,"durationMs":300}`,
		"",
	}, "\n")
	p, _ := ParserFor("")
	spans, err := p.Parse(strings.NewReader(in), "batch-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 3 {
		t.Fatalf("got %d spans", len(spans))
	}
	if spans[0].DurationNS() != int64(2*time.Second) {
		t.Fatalf("RFC3339 duration wrong: %v", spans[0].DurationNS())
	}
	if spans[1].DurationNS() != int64(1500*time.Millisecond) {
		t.Fatalf("epoch-ms duration wrong: %v", spans[1].DurationNS())
	}
	if spans[2].DurationNS() != int64(300*time.Millisecond) {
		t.Fatalf("start+durationMs wrong: %v", spans[2].DurationNS())
	}
	if spans[0].Source != "batch-1" {
		t.Fatalf("span must record source batch")
	}
}

func TestMissingIDRejected(t *testing.T) {
	p, _ := ParserFor("manual-jsonl")
	if _, err := p.Parse(strings.NewReader(`{"name":"x"}`), "b"); err == nil {
		t.Fatal("record without id must be rejected")
	}
}
