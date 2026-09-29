package ingest

import (
	"strings"
	"testing"
)

func TestManualImportPreservesRawEvidence(t *testing.T) {
	batch, err := Manual([]byte(`{
		"source":"on-call",
		"records":[{"id":"a","traceId":"t","service":"api","operation":"GET","deviceId":"host","start":"2026-09-29T10:00:00Z","end":"2026-09-29T10:00:01.250Z","attributes":{"ticket":"INC-1"}}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := batch.Records[0].DurationMS.Nominal; got != 1250 {
		t.Fatalf("duration = %v, want 1250", got)
	}
	if batch.Records[0].Raw["id"] != "a" || batch.Records[0].Attributes["ticket"] != "INC-1" {
		t.Fatal("raw evidence or attributes were not retained")
	}
	if !strings.HasPrefix(batch.ID, "batch-") {
		t.Fatalf("batch id = %q", batch.ID)
	}
}
