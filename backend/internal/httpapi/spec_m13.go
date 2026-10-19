package httpapi

import (
	"reflect"

	"github.com/cloudster1/exciter/backend/internal/openapi"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/risk"
)

// riskSchemas names the types of risk management (#258), whose short names would meet others', and spells their
// enums.
func riskSchemas(b *openapi.Builder) {
	names := map[reflect.Type]string{
		reflect.TypeFor[requirement.Side]():      "RiskRatingSide",
		reflect.TypeFor[requirement.Rating]():    "RiskRating",
		reflect.TypeFor[requirement.RiskLevel](): "RiskLevelRef",
		reflect.TypeFor[requirement.RiskView]():  "RiskRatingView",
		reflect.TypeFor[requirement.RiskMark]():  "RiskMark",
		reflect.TypeFor[risk.ScaleValue]():       "RiskScaleValue",
		reflect.TypeFor[risk.Scale]():            "RiskScale",
		reflect.TypeFor[risk.Level]():            "RiskLevel",
		reflect.TypeFor[risk.MatrixCell]():       "RiskMatrixCell",
		reflect.TypeFor[risk.Scheme]():           "RiskScheme",
		reflect.TypeFor[risk.ScaleInput]():       "RiskScaleInput",
		reflect.TypeFor[risk.SchemeInput]():      "RiskSchemeInput",
		reflect.TypeFor[risk.RiskRef]():          "RiskRef",
		reflect.TypeFor[risk.SourceRef]():        "RiskMitigationSource",
		reflect.TypeFor[risk.Mitigation]():       "RiskMitigation",
		reflect.TypeFor[risk.Mitigations]():      "RiskMitigations",
		reflect.TypeFor[risk.RowCan]():           "RiskRowCan",
		reflect.TypeFor[risk.Row]():              "RiskRow",
		reflect.TypeFor[risk.Rows]():             "RiskRows",
		reflect.TypeFor[risk.MatrixCount]():      "RiskMatrixCount",
		reflect.TypeFor[risk.Matrix]():           "RiskMatrix",
		reflect.TypeFor[risk.LevelCount]():       "RiskLevelCount",
		reflect.TypeFor[risk.Report]():           "RiskReport",
	}
	for t, name := range names {
		b.Names[t] = name
	}
	b.Enums[reflect.TypeOf(requirement.RiskState(""))] = enumStrings(requirement.RiskStates)
	b.Enums[reflect.TypeOf(risk.Method(""))] = enumStrings(risk.Methods)
	b.Enums[reflect.TypeOf(risk.Dimension(""))] = enumStrings(risk.Dimensions)
	b.Enums[reflect.TypeOf(risk.Side(""))] = enumStrings(risk.Sides)
}
