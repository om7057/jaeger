// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package querysvc

import (
	"go.opentelemetry.io/collector/featuregate"

	expression "github.com/jaegertracing/jaeger-idl/query/expression/v1"
	"github.com/jaegertracing/jaeger/internal/storage/v2/api/tracestore"
)

// StructuredFiltersGate admits the RFC 0005 structured query filter. It is on by default, and
// a deployment that disables it behaves exactly as it did before the filter existed, so an
// operator can withhold the filter AST while that RFC's remaining milestones settle it.
//
// It admits the filter into the query path; whether a backend evaluates one natively is a
// separate switch, named jaeger.<backend>.structuredFilters — this gate's leaf with the backend
// in place of "query" — because the two stabilize on different schedules.
var StructuredFiltersGate = featuregate.GlobalRegistry().MustRegister(
	"jaeger.query.structuredFilters",
	featuregate.StageBeta,
	featuregate.WithRegisterFromVersion("v2.21.0"),
	featuregate.WithRegisterDescription(
		"Accepts the RFC 0005 structured query filter on trace search. A query that carries one "+
			"is refused while this is disabled. This admits filters into the query path only; "+
			"whether a storage backend evaluates one natively is gated separately, by "+
			"jaeger.<backend>.structuredFilters.",
	),
	featuregate.WithRegisterReferenceURL("https://github.com/jaegertracing/jaeger/blob/main/docs/rfc/0005-structured-query-filters.md"),
)

// queryToReaderCapabilities returns the query in the shape the reader declared it can serve, immediately
// before dispatch.
//
// A reader that cannot paginate has no field to read PageSize from, so PageSize is folded into
// SearchDepth and Pagination is cleared, which keeps the search bounded. A PageToken is never
// folded: a reader that cannot paginate cannot have minted it, so the query is refused
// (RFC 0014 §6.2) rather than restarted as a new search.
//
// A reader that declares filter support gets the filter itself, once every level and operator
// it uses is one the reader listed. A reader that declares none gets the filter rewritten into
// the legacy predicate fields (ToLegacyShape), or a refusal where they cannot carry it.
func queryToReaderCapabilities(
	query tracestore.TraceQueryParams,
	caps tracestore.SearchCapabilities,
) (tracestore.TraceQueryParams, error) {
	if query.Pagination != nil && !caps.Paginated {
		if query.Pagination.PageToken != "" {
			return tracestore.TraceQueryParams{}, tracestore.ErrPaginationUnsupported
		}
		query.SearchDepth = query.Pagination.PageSize
		query.Pagination = nil
	}
	if query.Filter == nil {
		return query, nil
	}
	if caps.Filter.IsEmpty() {
		return query.ToLegacyShape()
	}
	if err := caps.Filter.EnsureSupported(query.Filter); err != nil {
		return query, err
	}
	// A reader that requires a service name (caps.WithoutServiceName false) is checked for one
	// right after this by the field this function does not touch for a declared filter: unlike
	// the legacy rewrite above, the filter reaches the reader as it is, predicates and all, so a
	// query naming the service only inside it reads as if it named none at all unless that name
	// is copied out here too.
	if query.ServiceName == "" && !caps.WithoutServiceName {
		query.ServiceName = serviceNameFromFilter(query.Filter)
	}
	return query, nil
}

// serviceNameFromFilter looks for an equality on the resource service field within a filter
// that is about to reach a reader unconverted (RFC 0005 §7). Every operator reaching here has
// already passed caps.Filter.EnsureSupported, so a reader that requires a service name but
// declares no operator wider than a flat `and`/`eq` conjunction (RFC 0005 M5) is one this walk
// can answer completely: the service name is the one leaf no wider operator could have hidden
// behind. A reader whose declared capability allows `or`, which could name the service on one
// branch only, is not told to require a service name in the first place (RFC 0005 M4).
func serviceNameFromFilter(filter *expression.Call) string {
	if filter == nil {
		return ""
	}
	if filter.Op == expression.OpAnd {
		for _, arg := range filter.Args {
			if call, ok := arg.(*expression.Call); ok {
				if name := serviceNameFromFilter(call); name != "" {
					return name
				}
			}
		}
		return ""
	}
	if filter.Op != expression.OpEq || len(filter.Args) != 2 {
		return ""
	}
	field, ok := filter.Args[0].(*expression.FieldRef)
	if !ok || field.Level != expression.LevelResource || field.Name != expression.ResourceFieldService {
		return ""
	}
	value, ok := filter.Args[1].(*expression.StringValue)
	if !ok {
		return ""
	}
	return value.Value
}
