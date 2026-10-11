// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package tracestore

import (
	"context"
	"iter"

	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/jaegertracing/jaeger/internal/storage/v1/api/spanstore"
	"github.com/jaegertracing/jaeger/internal/storage/v2/api/tracestore"
	"github.com/jaegertracing/jaeger/internal/storage/v2/v1adapter"
)

var _ tracestore.Reader = (*TraceReader)(nil)

// TraceReader declares Badger's filter capability boundary (RFC 0005 M5) over the shared v1
// adapter. The adapter (internal/storage/v2/v1adapter) is also used by Kafka's reader, which
// must not inherit a Badger-only declaration, so Badger gets this wrapper instead of a change
// to the shared adapter.
type TraceReader struct {
	*v1adapter.TraceReader
}

func NewTraceReader(spanReader spanstore.Reader) *TraceReader {
	return &TraceReader{TraceReader: v1adapter.NewTraceReader(spanReader)}
}

func (*TraceReader) SearchCapabilities(context.Context) (tracestore.SearchCapabilities, error) {
	filter := FilterCapabilities()
	return tracestore.SearchCapabilities{
		// The v1 reader rejects a query with no service name: Badger indexes by it.
		WithoutServiceName: false,
		// Each tag's index entry is keyed and scanned on its own and the results are
		// intersected by trace ID (ADR-005 §"Query Execution"), not evaluated against one
		// span, so a conjunction over several tags is answered at trace granularity.
		SameSpanConjunction: false,
		Filter:              &filter,
	}, nil
}

func (tr *TraceReader) FindTraces(
	ctx context.Context,
	query tracestore.TraceQueryParams,
) iter.Seq2[[]ptrace.Traces, error] {
	return func(yield func([]ptrace.Traces, error) bool) {
		lowered, err := lowerFilter(query)
		if err != nil {
			yield(nil, err)
			return
		}
		for traces, err := range tr.TraceReader.FindTraces(ctx, lowered) {
			if !yield(traces, err) {
				return
			}
		}
	}
}

func (tr *TraceReader) FindTraceIDs(
	ctx context.Context,
	query tracestore.TraceQueryParams,
) iter.Seq2[tracestore.PageChunk[[]tracestore.FoundTraceID], error] {
	return func(yield func(tracestore.PageChunk[[]tracestore.FoundTraceID], error) bool) {
		lowered, err := lowerFilter(query)
		if err != nil {
			yield(tracestore.PageChunk[[]tracestore.FoundTraceID]{}, err)
			return
		}
		for chunk, err := range tr.TraceReader.FindTraceIDs(ctx, lowered) {
			if !yield(chunk, err) {
				return
			}
		}
	}
}
