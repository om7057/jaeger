// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package tracestore

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jaegertracing/jaeger-idl/model/v1"
	expression "github.com/jaegertracing/jaeger-idl/query/expression/v1"
	spanstoremocks "github.com/jaegertracing/jaeger/internal/storage/v1/api/spanstore/mocks"
	"github.com/jaegertracing/jaeger/internal/storage/v2/api/tracestore"
)

func TestNewTraceReader(t *testing.T) {
	reader := &spanstoremocks.Reader{}
	tracereader := NewTraceReader(reader)
	require.NotNil(t, tracereader)
}

// Badger indexes every record by service name, so the wrapper declares that a search cannot
// omit it, and it declares the filter capability this package's FilterCapabilities states.
func TestTraceReader_SearchCapabilities(t *testing.T) {
	tracereader := NewTraceReader(&spanstoremocks.Reader{})
	caps, err := tracereader.SearchCapabilities(context.Background())
	require.NoError(t, err)
	filter := FilterCapabilities()
	assert.Equal(t, tracestore.SearchCapabilities{
		WithoutServiceName:  false,
		SameSpanConjunction: false,
		Filter:              &filter,
	}, caps)
}

func TestFindTraces_LowersAndDelegates(t *testing.T) {
	traceID := model.NewTraceID(0, 1)
	reader := &spanstoremocks.Reader{}
	reader.On("FindTraces", mock.Anything, mock.Anything).
		Return([]*model.Trace{{Spans: []*model.Span{{TraceID: traceID, OperationName: "op"}}}}, nil)
	tracereader := NewTraceReader(reader)

	var results int
	for batch, err := range tracereader.FindTraces(context.Background(), newTraceQueryParams()) {
		require.NoError(t, err)
		results += len(batch)
	}
	require.Equal(t, 1, results)
}

func TestFindTraces_StopIteration(t *testing.T) {
	traceID := model.NewTraceID(0, 1)
	reader := &spanstoremocks.Reader{}
	reader.On("FindTraces", mock.Anything, mock.Anything).
		Return([]*model.Trace{{Spans: []*model.Span{{TraceID: traceID, OperationName: "op"}}}}, nil)
	tracereader := NewTraceReader(reader)

	var count int
	for range tracereader.FindTraces(context.Background(), newTraceQueryParams()) {
		count++
		break
	}
	require.Equal(t, 1, count)
}

func TestFindTraces_Error(t *testing.T) {
	reader := &spanstoremocks.Reader{}
	reader.On("FindTraces", mock.Anything, mock.Anything).
		Return([]*model.Trace(nil), errors.New("find error"))
	tracereader := NewTraceReader(reader)

	for _, err := range tracereader.FindTraces(context.Background(), newTraceQueryParams()) {
		require.ErrorContains(t, err, "find error")
	}
}

// The refusal must come from this wrapper's own lowering, before the v1 reader is ever asked:
// a backend that declares this filter capability never gets a request it cannot carry.
func TestFindTraces_FilterUnsupported(t *testing.T) {
	reader := &spanstoremocks.Reader{}
	tracereader := NewTraceReader(reader)
	query := newTraceQueryParams()
	query.Filter = &expression.Call{
		Op: expression.OpOr,
		Args: []expression.Expression{
			&expression.Call{Op: expression.OpEq, Args: []expression.Expression{
				&expression.AttributeRef{Key: "a"}, &expression.AnyValue{Value: "1"},
			}},
			&expression.Call{Op: expression.OpEq, Args: []expression.Expression{
				&expression.AttributeRef{Key: "b"}, &expression.AnyValue{Value: "2"},
			}},
		},
	}

	var called bool
	for _, err := range tracereader.FindTraces(context.Background(), query) {
		called = true
		require.ErrorIs(t, err, tracestore.ErrFilterUnsupported)
	}
	require.True(t, called, "the refusal must be yielded rather than the sequence silently ending")
	reader.AssertNotCalled(t, "FindTraces", mock.Anything, mock.Anything)
}

func TestFindTraceIDs_LowersAndDelegates(t *testing.T) {
	traceID := model.NewTraceID(0, 1)
	reader := &spanstoremocks.Reader{}
	reader.On("FindTraceIDs", mock.Anything, mock.Anything).Return([]model.TraceID{traceID}, nil)
	tracereader := NewTraceReader(reader)

	var results []tracestore.FoundTraceID
	for chunk, err := range tracereader.FindTraceIDs(context.Background(), newTraceQueryParams()) {
		require.NoError(t, err)
		assert.Empty(t, chunk.NextPageToken)
		results = append(results, chunk.Results...)
	}
	require.Len(t, results, 1)
}

func TestFindTraceIDs_StopIteration(t *testing.T) {
	traceID1 := model.NewTraceID(0, 1)
	traceID2 := model.NewTraceID(0, 2)
	reader := &spanstoremocks.Reader{}
	reader.On("FindTraceIDs", mock.Anything, mock.Anything).Return([]model.TraceID{traceID1, traceID2}, nil)
	tracereader := NewTraceReader(reader)

	var count int
	for range tracereader.FindTraceIDs(context.Background(), newTraceQueryParams()) {
		count++
		break
	}
	require.Equal(t, 1, count)
}

func TestFindTraceIDs_Error(t *testing.T) {
	reader := &spanstoremocks.Reader{}
	reader.On("FindTraceIDs", mock.Anything, mock.Anything).
		Return([]model.TraceID(nil), errors.New("find error"))
	tracereader := NewTraceReader(reader)

	for _, err := range tracereader.FindTraceIDs(context.Background(), newTraceQueryParams()) {
		require.ErrorContains(t, err, "find error")
	}
}

func TestFindTraceIDs_FilterUnsupported(t *testing.T) {
	reader := &spanstoremocks.Reader{}
	tracereader := NewTraceReader(reader)
	query := newTraceQueryParams()
	query.Filter = &expression.Call{
		Op: expression.OpEq,
		Args: []expression.Expression{
			&expression.AttributeRef{Key: "zone", Level: expression.LevelScope},
			&expression.AnyValue{Value: "us-east"},
		},
	}

	var called bool
	for _, err := range tracereader.FindTraceIDs(context.Background(), query) {
		called = true
		require.ErrorIs(t, err, tracestore.ErrFilterUnsupported)
	}
	require.True(t, called, "the refusal must be yielded rather than the sequence silently ending")
	reader.AssertNotCalled(t, "FindTraceIDs", mock.Anything, mock.Anything)
}
