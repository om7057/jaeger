// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/jaegertracing/jaeger/internal/storage/v2/api/tracestore"
	"github.com/jaegertracing/jaeger/internal/telemetry/otelsemconv"
)

// completeSearchService is the one service of the three completeSearch spans that
// testFindCompleteTraces actually searches by; the other two exist only to be returned alongside
// it, so completeSearchMatch finds this one by name rather than assuming its position in the trace.
const completeSearchService = "complete-trace-search"

func buildCompleteSearchTrace() ptrace.Traces {
	trace := ptrace.NewTraces()
	// Keep shared fixtures within the microsecond precision supported by all backends.
	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	for i, fixture := range []struct {
		service string
		offset  time.Duration
	}{
		{service: "complete-trace-other", offset: 123456 * time.Microsecond},
		{service: completeSearchService, offset: time.Second + 123456*time.Microsecond},
		{service: "complete-trace-other", offset: 3*time.Second + 987654*time.Microsecond},
	} {
		rs := trace.ResourceSpans().AppendEmpty()
		rs.Resource().Attributes().PutStr(otelsemconv.ServiceNameKey, fixture.service)
		span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
		span.SetTraceID(pcommon.TraceID{0xC1, 0xF1})
		// Avoid span ID collisions when ingestion batches this trace with the rest of the corpus.
		span.SetSpanID(pcommon.SpanID{0xC1, 0xF1, byte(i + 1)})
		span.SetName("complete-trace-operation")
		start := base.Add(fixture.offset)
		span.SetStartTimestamp(pcommon.NewTimestampFromTime(start))
		span.SetEndTimestamp(pcommon.NewTimestampFromTime(start.Add(time.Microsecond)))
	}
	return trace
}

// completeSearchMatch returns the completeSearchService span of trace, rather than assuming its
// position: buildCompleteSearchTrace and this test should not have to agree on fixture order.
func completeSearchMatch(t *testing.T, trace ptrace.Traces) ptrace.Span {
	for i := 0; i < trace.ResourceSpans().Len(); i++ {
		rs := trace.ResourceSpans().At(i)
		if name, ok := rs.Resource().Attributes().Get(otelsemconv.ServiceNameKey); ok && name.Str() == completeSearchService {
			return rs.ScopeSpans().At(0).Spans().At(0)
		}
	}
	require.Fail(t, "fixture has no span for service "+completeSearchService)
	return ptrace.Span{}
}

func (s *StorageIntegration) testFindCompleteTraces(t *testing.T) {
	s.skipIfNeeded(t)
	expected := s.Corpus.CompleteSearch
	matched := completeSearchMatch(t, expected)
	start := matched.StartTimestamp().AsTime().Truncate(time.Second)
	query := &tracestore.TraceQueryParams{
		ServiceName:  completeSearchService,
		Attributes:   pcommon.NewMap(),
		StartTimeMin: start,
		StartTimeMax: start.Add(time.Second),
		SearchDepth:  1,
	}
	actual := s.findTracesByQuery(t, query, []ptrace.Traces{expected})
	for _, trace := range actual {
		CompareTraces(t, expected, trace)
	}
}
