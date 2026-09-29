package ingest

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"performance-investigation/internal/model"
)

type ManualImport struct {
	Source      string         `json:"source"`
	Description string         `json:"description"`
	Records     []ManualRecord `json:"records"`
}

type ManualRecord struct {
	ID         string         `json:"id"`
	TraceID    string         `json:"traceId"`
	ParentID   string         `json:"parentId"`
	Service    string         `json:"service"`
	Operation  string         `json:"operation"`
	DeviceID   string         `json:"deviceId"`
	Start      string         `json:"start"`
	End        string         `json:"end"`
	Attributes map[string]any `json:"attributes"`
}

func Manual(data []byte) (model.Batch, error) {
	var input ManualImport
	if err := json.Unmarshal(data, &input); err != nil {
		return model.Batch{}, err
	}
	if len(input.Records) == 0 {
		return model.Batch{}, fmt.Errorf("records is required")
	}
	batch := model.Batch{ID: newID("batch"), Source: input.Source, Format: "manual-calls-v1", ImportedAt: time.Now().UTC(), Description: input.Description}
	if batch.Source == "" {
		batch.Source = "manual"
	}
	for index, item := range input.Records {
		start, err := parseTime(item.Start)
		if err != nil {
			return model.Batch{}, fmt.Errorf("record %d start: %w", index, err)
		}
		end, err := parseTime(item.End)
		if err != nil {
			return model.Batch{}, fmt.Errorf("record %d end: %w", index, err)
		}
		id := item.ID
		if id == "" {
			id = fmt.Sprintf("rec-%d", index+1)
		}
		traceID := item.TraceID
		if traceID == "" {
			traceID = "trace-1"
		}
		deviceID := item.DeviceID
		if deviceID == "" {
			deviceID = "local"
		}
		raw := map[string]any{}
		jsonBody, _ := json.Marshal(item)
		_ = json.Unmarshal(jsonBody, &raw)
		record := model.CallRecord{ID: id, TraceID: traceID, ParentID: item.ParentID, Service: item.Service, Operation: item.Operation, DeviceID: deviceID, Start: start, End: end, Attributes: item.Attributes, Raw: raw}
		record.DurationMS = model.DurationBreakdown{Nominal: float64(end.Sub(start).Microseconds()) / 1000, Certain: true}
		record.Evidence = []model.Evidence{{Type: "manual_record", Summary: "人工录入的开始/结束时间戳", Detail: map[string]any{"source": batch.Source, "deviceId": deviceID}}}
		batch.Records = append(batch.Records, record)
	}
	return batch, nil
}

func parseTime(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05.000"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported timestamp %q", value)
}

func newID(prefix string) string {
	bytes := make([]byte, 8)
	_, _ = rand.Read(bytes)
	return prefix + "-" + hex.EncodeToString(bytes)
}
