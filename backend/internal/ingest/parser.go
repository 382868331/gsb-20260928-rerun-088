package ingest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"perfinvest/internal/model"
)

// Parser converts one collection-format file into spans.
// New collection formats register here later without touching the API.
type Parser interface {
	Format() string
	Parse(r io.Reader, batchID string) ([]model.Span, error)
}

var registry = map[string]Parser{
	"manual-jsonl": manualJSONL{},
}

// ParserFor returns the parser for a format; empty name defaults to manual-jsonl.
func ParserFor(format string) (Parser, error) {
	if format == "" {
		format = "manual-jsonl"
	}
	p, ok := registry[format]
	if !ok {
		return nil, fmt.Errorf("unsupported import format %q", format)
	}
	return p, nil
}

// SupportedFormats lists the registered collection formats.
func SupportedFormats() []string {
	out := make([]string, 0, len(registry))
	for f := range registry {
		out = append(out, f)
	}
	return out
}

type manualJSONL struct{}

func (manualJSONL) Format() string { return "manual-jsonl" }

func (manualJSONL) Parse(r io.Reader, batchID string) ([]model.Span, error) {
	var spans []model.Span
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			return nil, fmt.Errorf("line %d: invalid JSON: %w", lineNo, err)
		}
		span, err := decodeManual(raw)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		span.Source = batchID
		span.Seq = lineNo
		spans = append(spans, span)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(spans) == 0 {
		return nil, fmt.Errorf("no call records found")
	}
	return spans, nil
}

func strField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch t := v.(type) {
			case string:
				if s := strings.TrimSpace(t); s != "" {
					return s
				}
			case float64:
				return strconv.FormatInt(int64(t), 10)
			}
		}
	}
	return ""
}

func decodeManual(m map[string]any) (model.Span, error) {
	s := model.Span{Raw: m}
	s.ID = strField(m, "id", "spanId", "span_id")
	s.ParentID = strField(m, "parentId", "parent_id", "parentSpanId")
	s.TraceID = strField(m, "traceId", "trace_id")
	s.Service = strField(m, "service", "serviceName")
	s.Name = strField(m, "name", "operation", "op", "endpoint")
	s.Kind = strField(m, "kind", "spanKind")
	s.Device = strField(m, "device", "host", "node")
	if s.ID == "" {
		return s, fmt.Errorf("missing span id")
	}
	if s.Name == "" {
		s.Name = s.ID
	}

	start, sok := timeField(m, "start", "startTime", "start_time", "startNs", "start_ns", "startMs", "start_ms", "startUs", "start_us")
	end, eok := timeField(m, "end", "endTime", "end_time", "endNs", "end_ns", "endMs", "end_ms", "endUs", "end_us")
	if !sok || !eok {
		// fall back to start + duration fields
		dur, dok := durationField(m)
		if sok && dok {
			s.StartNS, s.EndNS = start, start+dur
			return s, nil
		}
		return s, fmt.Errorf("span %q missing start/end times", s.ID)
	}
	s.StartNS, s.EndNS = start, end
	return s, nil
}

func timeField(m map[string]any, keys ...string) (int64, bool) {
	for _, k := range keys {
		v, ok := m[k]
		if !ok {
			continue
		}
		l := strings.ToLower(k)
		switch n := v.(type) {
		case string:
			if ts, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(n)); err == nil {
				return ts.UnixNano(), true
			}
			if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
				return scaleNumber(f, l), true
			}
		case float64:
			return scaleNumber(n, l), true
		}
	}
	return 0, false
}

func scaleNumber(f float64, key string) int64 {
	n := int64(f)
	switch {
	case strings.Contains(key, "ms"):
		return n * int64(time.Millisecond)
	case strings.Contains(key, "us"):
		return n * int64(time.Microsecond)
	case strings.Contains(key, "ns"):
		return n
	default:
		// Heuristic: absolute epoch values are recognized by magnitude.
		if n < 1e12 { // seconds
			return n * int64(time.Second)
		}
		if n < 1e15 { // milliseconds
			return n * int64(time.Millisecond)
		}
		if n < 1e18 { // microseconds
			return n * int64(time.Microsecond)
		}
		return n
	}
}

func durationField(m map[string]any) (int64, bool) {
	for _, k := range []string{"duration", "durationMs", "duration_ms", "durationNs", "duration_ns", "dur"} {
		v, ok := m[k]
		if !ok {
			continue
		}
		l := strings.ToLower(k)
		switch n := v.(type) {
		case float64:
			if strings.Contains(l, "ms") {
				return int64(n) * int64(time.Millisecond), true
			}
			if strings.Contains(l, "ns") {
				return int64(n), true
			}
			if n < 1e6 {
				return int64(n * float64(time.Millisecond)), true
			}
			return int64(n), true
		case string:
			if d, err := time.ParseDuration(n); err == nil {
				return d.Nanoseconds(), true
			}
		}
	}
	return 0, false
}
