package httpapi

import (
	"reflect"
	"slices"

	"github.com/cloudster1/exciter/backend/internal/armature"
	"github.com/cloudster1/exciter/backend/internal/baseline"
	"github.com/cloudster1/exciter/backend/internal/openapi"
	"github.com/cloudster1/exciter/backend/internal/stator"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// traceabilitySchemas names the types of traceability (#146), whose packages reuse short names such as Model and
// Matrix, and spells their enums.
func traceabilitySchemas(b *openapi.Builder) {
	names := map[reflect.Type]string{
		reflect.TypeFor[trace.ModelStep]():       "TraceModelStep",
		reflect.TypeFor[trace.ModelLevel]():      "TraceModelLevel",
		reflect.TypeFor[trace.ModelCan]():        "TraceModelCan",
		reflect.TypeFor[trace.Model]():           "TraceModel",
		reflect.TypeFor[trace.ModelLevelInput](): "TraceModelLevelInput",
		reflect.TypeFor[trace.ModelInput]():      "TraceModelInput",
		reflect.TypeFor[trace.ChainNode]():       "TraceChainNode",
		reflect.TypeFor[trace.ChainLine]():       "TraceChainLine",
		reflect.TypeFor[trace.Chains]():          "TraceChains",
		reflect.TypeFor[trace.MatrixPage]():      "MatrixPage",
		reflect.TypeFor[baseline.MatrixLink]():   "BaselineMatrixLink",
		reflect.TypeFor[baseline.MatrixCell]():   "BaselineMatrixCell",
		reflect.TypeFor[baseline.Matrix]():       "BaselineMatrix",
	}
	for t, name := range names {
		b.Names[t] = name
	}
	b.Enums[reflect.TypeOf(trace.LevelKind(""))] = enumStrings(trace.LevelKinds)
	b.Enums[reflect.TypeOf(trace.LinkKind(""))] = enumStrings(trace.LinkKinds)
	b.Enums[reflect.TypeOf(trace.MatrixDirection(""))] = enumStrings(trace.MatrixDirections)
	relations := append(enumStrings(armature.Relations), enumStrings(stator.Relations)...)
	b.FieldOverrides["TraceModelStep.relation"] = &openapi.Schema{Type: "string", Enum: slices.Compact(slices.Sorted(slices.Values(relations)))}
	b.FieldOverrides["BaselineMatrixLink.status"] = openapi.Nullable(&openapi.Schema{Type: "string", Enum: armature.StatusCategories})
}
