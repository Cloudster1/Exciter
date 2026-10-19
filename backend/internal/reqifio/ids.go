package reqifio

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/trace"
)

// The identifiers an export writes for what no earlier exchange named, each a prefix and the id of the row, so
// a file that comes back finds the row again without having been imported first.
const (
	prefixObject   = "exciter-req-"
	prefixType     = "exciter-type-"
	prefixAttr     = "exciter-attr-"
	prefixField    = "exciter-field-"
	prefixOption   = "exciter-option-"
	prefixStatus   = "exciter-status-"
	prefixPriority = "exciter-priority-"
	prefixLinkType = "exciter-linktype-"
	prefixTrace    = "exciter-trace-"
	prefixProject  = "exciter-project-"
	prefixNode     = "exciter-node-"
	prefixDataEnum = "exciter-dt-enum-"
	prefixExport   = "exciter-export-"

	sectionType       = "exciter-type-section"
	untypedType       = "exciter-type-none"
	specificationType = "exciter-type-specification"
	statusEnum        = "exciter-dt-status"
	priorityEnum      = "exciter-dt-priority"
)

// The glossary an export carries (#210): a spec type of terms, one object per term, and a specification of its own,
// which an import leaves out.
const (
	glossaryTermType = "exciter-glossary-term"
	glossarySpecType = "exciter-type-glossary"
	prefixGlossary   = "exciter-glossary-"
	prefixTerm       = "exciter-term-"
	prefixTermNode   = "exciter-node-term-"
)

// The attributes of a term, each defined as the glossary spec type's.
const (
	termName       = prefixGlossary + "name"
	termDefinition = prefixGlossary + "definition"
	termSynonyms   = prefixGlossary + "synonyms"
	termAvoid      = prefixGlossary + "avoid"
	termStatus     = prefixGlossary + "status"
)

// The fields every requirement has, which an attribute of the file may fill instead of an attribute.
const (
	fieldKey       = "key"
	fieldTitle     = "title"
	fieldChapter   = "chapter"
	fieldStatement = "statement"
	fieldRationale = "rationale"
	fieldStatus    = "status"
	fieldPriority  = "priority"
	fieldOwner     = "owner"
	fieldLabels    = "labels"
	// fieldPreconditions, fieldSteps and fieldParameters are what a test case carries (#250).
	fieldPreconditions = "preconditions"
	fieldSteps         = "steps"
	fieldParameters    = "parameters"
	// fieldRisk is a risk's rating (#259), as requirement.RatingText writes it.
	fieldRisk = "risk"
)

// The attribute names of the ReqIF Implementation Guide that most tools write, and what each fills.
var standardFields = map[string]string{
	"ReqIF.ForeignID":   fieldKey,
	"ReqIF.Name":        fieldTitle,
	"ReqIF.ChapterName": fieldChapter,
	"ReqIF.Text":        fieldStatement,
}

// fieldNames are what an export calls the fields, the standard ones first.
var fieldNames = map[string]string{
	fieldKey: "ReqIF.ForeignID", fieldTitle: "ReqIF.Name", fieldChapter: "ReqIF.ChapterName", fieldStatement: "ReqIF.Text",
	fieldRationale: "Rationale", fieldStatus: "Status", fieldPriority: "Priority", fieldOwner: "Owner", fieldLabels: "Labels",
	fieldPreconditions: "Preconditions", fieldSteps: "Steps", fieldParameters: "Parameters", fieldRisk: "Risk",
}

// fieldOf is the field an attribute definition fills, empty for an attribute of its own.
func fieldOf(d *AttrDef) string {
	if rest, ok := strings.CutPrefix(d.Identifier, prefixField); ok {
		if i := strings.LastIndex(rest, "-"); i >= 0 {
			if _, known := fieldNames[rest[i+1:]]; known {
				return rest[i+1:]
			}
		}
	}
	return standardFields[d.Name]
}

func fieldIdentifier(scope, field string) string { return prefixField + scope + "-" + field }

func attrIdentifier(attr, typeID uuid.UUID) string {
	return prefixAttr + attr.String() + "-" + typeID.String()
}

// attrOf reads the attribute an export's attribute identifier names.
func attrOf(identifier string) (uuid.UUID, bool) {
	rest, ok := strings.CutPrefix(identifier, prefixAttr)
	if !ok || len(rest) < len(uuid.Nil.String()) {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(rest[:len(uuid.Nil.String())])
	return id, err == nil
}

var notKeyish = regexp.MustCompile(`[^a-z0-9]+`)

// keyOf makes a link type's key of a name: lower case, words joined by underscores, a letter first.
func keyOf(name string, taken func(string) bool) string {
	key := strings.Trim(notKeyish.ReplaceAllString(strings.ToLower(name), "_"), "_")
	if key == "" || key[0] < 'a' || key[0] > 'z' {
		key = "imported_" + key
	}
	if len(key) < 2 {
		key += "_link"
	}
	key = strings.TrimRight(truncate(key, trace.MaxKeyLength), "_")
	base := key
	for n := 2; taken(key); n++ {
		suffix := "_" + strconv.Itoa(n)
		key = truncate(base, trace.MaxKeyLength-len(suffix)) + suffix
	}
	return key
}
