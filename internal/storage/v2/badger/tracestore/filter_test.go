// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package tracestore

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"

	expression "github.com/jaegertracing/jaeger-idl/query/expression/v1"
	"github.com/jaegertracing/jaeger/internal/storage/v2/api/tracestore"
)

func TestFilterCapabilities(t *testing.T) {
	caps := FilterCapabilities()
	assert.ElementsMatch(t, []expression.Level{
		expression.LevelSpan,
		expression.LevelResource,
		expression.LevelEvent,
	}, caps.Levels)
	assert.ElementsMatch(t, []expression.Operator{
		expression.OpAnd,
		expression.OpEq,
	}, caps.Operators)
}

func newTraceQueryParams() tracestore.TraceQueryParams {
	return tracestore.TraceQueryParams{
		ServiceName:  "service-1",
		StartTimeMin: time.Now().Add(-time.Hour),
		StartTimeMax: time.Now(),
		Attributes:   pcommon.NewMap(),
	}
}

func TestLowerFilter_NoFilter(t *testing.T) {
	query := newTraceQueryParams()
	lowered, err := lowerFilter(query)
	require.NoError(t, err)
	assert.Equal(t, query, lowered)
}

func TestLowerFilter_SupportedConjunction(t *testing.T) {
	query := tracestore.TraceQueryParams{
		StartTimeMin: time.Now().Add(-time.Hour),
		StartTimeMax: time.Now(),
	}
	query.Filter = &expression.Call{
		Op: expression.OpAnd,
		Args: []expression.Expression{
			&expression.Call{
				Op: expression.OpEq,
				Args: []expression.Expression{
					&expression.FieldRef{Level: expression.LevelResource, Name: expression.ResourceFieldService},
					&expression.StringValue{Value: "service-1"},
				},
			},
			&expression.Call{
				Op: expression.OpEq,
				Args: []expression.Expression{
					&expression.AttributeRef{Key: "http.status_code"},
					&expression.AnyValue{Value: "500"},
				},
			},
		},
	}
	lowered, err := lowerFilter(query)
	require.NoError(t, err)
	assert.Nil(t, lowered.Filter)
	assert.Equal(t, "service-1", lowered.ServiceName)
	value, ok := lowered.Attributes.Get("http.status_code")
	require.True(t, ok)
	assert.Equal(t, "500", value.AsString())
}

// TestLowerFilter_IgnoresAPreCopiedServiceName pins that a service name the query service
// already copied onto the legacy field (to satisfy its own required-service-name check for a
// declared filter, RFC 0005 §7) is not read as a second predicate on the field when the same
// name is also the filter's own service predicate.
func TestLowerFilter_IgnoresAPreCopiedServiceName(t *testing.T) {
	query := newTraceQueryParams()
	query.ServiceName = "service-1"
	query.Filter = serviceIs("service-1")
	lowered, err := lowerFilter(query)
	require.NoError(t, err)
	assert.Equal(t, "service-1", lowered.ServiceName)
	assert.Nil(t, lowered.Filter)
}

func serviceIs(name string) *expression.Call {
	return &expression.Call{Op: expression.OpEq, Args: []expression.Expression{
		&expression.FieldRef{Level: expression.LevelResource, Name: expression.ResourceFieldService},
		&expression.StringValue{Value: name},
	}}
}

func TestLowerFilter_RefusesOr(t *testing.T) {
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
	_, err := lowerFilter(query)
	require.ErrorIs(t, err, tracestore.ErrFilterUnsupported)
}

func TestLowerFilter_RefusesUnindexedLevel(t *testing.T) {
	query := newTraceQueryParams()
	query.Filter = &expression.Call{
		Op: expression.OpEq,
		Args: []expression.Expression{
			&expression.AttributeRef{Key: "zone", Level: expression.LevelScope},
			&expression.AnyValue{Value: "us-east"},
		},
	}
	_, err := lowerFilter(query)
	require.ErrorIs(t, err, tracestore.ErrFilterUnsupported)
}
