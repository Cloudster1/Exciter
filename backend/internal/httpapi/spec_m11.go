package httpapi

import (
	"reflect"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/attribute/formula"
	"github.com/cloudster1/exciter/backend/internal/dashboard"
	"github.com/cloudster1/exciter/backend/internal/exporttemplate"
	"github.com/cloudster1/exciter/backend/internal/openapi"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// analysisSchemas names the types of analysis and reporting (#242), whose short names would meet others', and spells
// their enums.
func analysisSchemas(b *openapi.Builder) {
	names := map[reflect.Type]string{
		reflect.TypeFor[trace.GraphNode]():         "TraceGraphNode",
		reflect.TypeFor[trace.GraphEdge]():         "TraceGraphEdge",
		reflect.TypeFor[trace.Graph]():             "TraceGraph",
		reflect.TypeFor[attribute.Recompute]():     "AttributeRecompute",
		reflect.TypeFor[formula.Error]():           "FormulaError",
		reflect.TypeFor[formula.CheckInput]():      "FormulaCheckInput",
		reflect.TypeFor[formula.Check]():           "FormulaCheck",
		reflect.TypeFor[dashboard.KindInfo]():      "WidgetKindInfo",
		reflect.TypeFor[dashboard.Source]():        "WidgetSource",
		reflect.TypeFor[dashboard.Config]():        "WidgetConfig",
		reflect.TypeFor[dashboard.Widget]():        "Widget",
		reflect.TypeFor[dashboard.Can]():           "DashboardCan",
		reflect.TypeFor[dashboard.Summary]():       "DashboardSummary",
		reflect.TypeFor[dashboard.Dashboard]():     "Dashboard",
		reflect.TypeFor[dashboard.Input]():         "DashboardInput",
		reflect.TypeFor[dashboard.Patch]():         "DashboardPatch",
		reflect.TypeFor[dashboard.WidgetInput]():   "WidgetInput",
		reflect.TypeFor[dashboard.WidgetPatch]():   "WidgetPatch",
		reflect.TypeFor[dashboard.OrderInput]():    "WidgetOrderInput",
		reflect.TypeFor[dashboard.PreviewInput]():  "WidgetPreviewInput",
		reflect.TypeFor[dashboard.Bucket]():        "WidgetBucket",
		reflect.TypeFor[dashboard.Group]():         "WidgetGroup",
		reflect.TypeFor[dashboard.Point]():         "WidgetPoint",
		reflect.TypeFor[dashboard.Line]():          "WidgetLine",
		reflect.TypeFor[dashboard.Data]():          "WidgetData",
		reflect.TypeFor[dashboard.Template]():      "DashboardTemplate",
		reflect.TypeFor[exporttemplate.FieldRef](): "ExportTemplateField",
		reflect.TypeFor[exporttemplate.Include]():  "ExportTemplateInclude",
		reflect.TypeFor[exporttemplate.Can]():      "ExportTemplateCan",
		reflect.TypeFor[exporttemplate.Template](): "ExportTemplate",
		reflect.TypeFor[exporttemplate.Form]():     "ExportTemplateForm",
		reflect.TypeFor[exporttemplate.Patch]():    "ExportTemplatePatch",
	}
	for t, name := range names {
		b.Names[t] = name
	}
	b.Enums[reflect.TypeOf(trace.GraphDirection(""))] = enumStrings(trace.GraphDirections)
	b.Enums[reflect.TypeOf(trace.NodeKind(""))] = enumStrings(trace.NodeKinds)
	b.Enums[reflect.TypeOf(formula.Kind(""))] = enumStrings(formula.Kinds)
	b.Enums[reflect.TypeOf(dashboard.Kind(""))] = enumStrings(dashboard.Kinds)
	b.Enums[reflect.TypeOf(dashboard.GroupBy(""))] = enumStrings(dashboard.GroupBys)
	b.Enums[reflect.TypeOf(dashboard.Shape(""))] = enumStrings(dashboard.Shapes)
	b.Enums[reflect.TypeOf(dashboard.Interval(""))] = enumStrings(dashboard.Intervals)
	b.Enums[reflect.TypeOf(dashboard.Series(""))] = enumStrings(dashboard.SeriesKinds)
	b.Enums[reflect.TypeOf(exporttemplate.Placeholder(""))] = enumStrings(exporttemplate.Placeholders)
	b.Enums[reflect.TypeOf(exporttemplate.Field(""))] = enumStrings(exporttemplate.Fields)
}
