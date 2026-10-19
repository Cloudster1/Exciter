// Package risk keeps a project's risk scheme and what follows from it (#259): the levels its ratings give, from
// #260 mitigations, the FMEA table and the risk matrix, and from #261 the report. See docs/api-contract-m13.md.
package risk

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// Agreed limits of a risk scheme and the FMEA table (#258).
const (
	MaxScaleValue        = 10
	MaxLevels            = 10
	MaxNameLength        = 100
	MaxLabelLength       = 50
	MaxDescriptionLength = 500
	MaxRows              = 200
	DefaultRows          = 50
)

// Method is how a scheme gives a level: a cell of its matrix, or the priority number reaching a threshold.
type Method string

const (
	MethodMatrix Method = "matrix"
	MethodRPN    Method = "rpn"
)

// Methods lists every Method, for the API document.
var Methods = []Method{MethodMatrix, MethodRPN}

// Dimension is a scale a risk is rated on.
type Dimension string

const (
	Severity    Dimension = "severity"
	Probability Dimension = "probability"
	Detection   Dimension = "detection"
)

// Dimensions lists every Dimension, for the API document.
var Dimensions = []Dimension{Severity, Probability, Detection}

// Side is which rating the FMEA table and the matrix read: before mitigation, or the one in effect.
type Side string

const (
	SideBefore    Side = "before"
	SideEffective Side = "effective"
)

// Sides lists every Side, for the API document.
var Sides = []Side{SideBefore, SideEffective}

// ScaleValue is one value of a scale, named; higher is worse.
type ScaleValue struct {
	Value       int    `json:"value"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Scale is one dimension of a scheme, with the label the project gives it and its values in order.
type Scale struct {
	Dimension Dimension    `json:"dimension"`
	Label     string       `json:"label"`
	Values    []ScaleValue `json:"values"`
	// Held are the values a rating uses, which stay while it does.
	Held []int `json:"held"`
}

// Level is a level of a scheme, from the least to the most severe; MinRPN is where it starts by priority number.
type Level struct {
	Name       string `json:"name"`
	Colour     string `json:"colour"`
	Acceptable bool   `json:"acceptable"`
	MinRPN     *int   `json:"minRpn"`
}

// MatrixCell names the level of one severity and probability.
type MatrixCell struct {
	Severity    int    `json:"severity"`
	Probability int    `json:"probability"`
	Level       string `json:"level"`
}

// Scheme is a project's risk scheme.
type Scheme struct {
	Method    Method                `json:"method"`
	Detection bool                  `json:"detection"`
	Scales    []Scale               `json:"scales"`
	Levels    []Level               `json:"levels"`
	Matrix    []MatrixCell          `json:"matrix"`
	UpdatedBy requirement.PersonRef `json:"updatedBy"`
	UpdatedAt time.Time             `json:"updatedAt"`
}

// ScaleInput is a scale as administrators set it.
type ScaleInput struct {
	Dimension Dimension    `json:"dimension"`
	Label     string       `json:"label"`
	Values    []ScaleValue `json:"values"`
}

// SchemeInput is a whole scheme as administrators set it; the matrix names its levels by name.
type SchemeInput struct {
	Method    Method       `json:"method"`
	Detection bool         `json:"detection"`
	Scales    []ScaleInput `json:"scales"`
	Levels    []Level      `json:"levels"`
	Matrix    []MatrixCell `json:"matrix"`
}

// RiskRef is a risk as the FMEA table, the matrix and the report list it.
type RiskRef struct {
	ID       uuid.UUID           `json:"id"`
	Key      string              `json:"key"`
	Title    string              `json:"title"`
	Revision int                 `json:"revision"`
	Type     requirement.TypeRef `json:"type"`
}

// SourceRef is a requirement that mitigates, perhaps in another project.
type SourceRef struct {
	ID         uuid.UUID `json:"id"`
	Key        string    `json:"key"`
	Title      string    `json:"title"`
	ProjectKey string    `json:"projectKey"`
}

// Mitigation is one requirement mitigating a risk, with what its project's coverage rules say of it (#260).
type Mitigation struct {
	LinkID      uuid.UUID   `json:"linkId"`
	Source      SourceRef   `json:"source"`
	Implemented trace.Level `json:"implemented"`
	Verified    trace.Level `json:"verified"`
	Suspect     bool        `json:"suspect"`
	InEffect    bool        `json:"inEffect"`
}

// Mitigations are a risk's mitigations and the state they put it in.
type Mitigations struct {
	Mitigations []Mitigation          `json:"mitigations"`
	State       requirement.RiskState `json:"state"`
}

// RowCan says what the reader may change of a row of the FMEA table.
type RowCan struct {
	Edit bool `json:"edit"`
}

// Row is one risk of the FMEA table: its attributes, its rating with what follows from it, and its mitigations.
type Row struct {
	Risk        RiskRef                    `json:"risk"`
	Attributes  map[string]json.RawMessage `json:"attributes"`
	Rating      requirement.RiskView       `json:"rating"`
	Mitigations []Mitigation               `json:"mitigations"`
	Can         RowCan                     `json:"can"`
}

// Rows are a page of the FMEA table, with how many risks match and how many of those are unrated.
type Rows struct {
	Risks   []Row `json:"risks"`
	Total   int   `json:"total"`
	Unrated int   `json:"unrated"`
}

// MatrixCount is how many risks fall in one cell of the risk matrix, and the level the matrix gives it.
type MatrixCount struct {
	Severity    int                    `json:"severity"`
	Probability int                    `json:"probability"`
	Level       *requirement.RiskLevel `json:"level"`
	Count       int                    `json:"count"`
}

// Matrix is the risk matrix of one side: severity across, probability down.
type Matrix struct {
	Side        Side          `json:"side"`
	Severity    Scale         `json:"severity"`
	Probability Scale         `json:"probability"`
	Cells       []MatrixCount `json:"cells"`
	Unrated     int           `json:"unrated"`
}

// LevelCount is how many risks stand at a level, before mitigation and in effect.
type LevelCount struct {
	Level     requirement.RiskLevel `json:"level"`
	Before    int                   `json:"before"`
	Effective int                   `json:"effective"`
}

// Report is what the risk report holds (#261).
type Report struct {
	ProjectKey  string       `json:"projectKey"`
	ProjectName string       `json:"projectName"`
	Scheme      Scheme       `json:"scheme"`
	Levels      []LevelCount `json:"levels"`
	Unrated     int          `json:"unrated"`
	Risks       []Row        `json:"risks"`
}

// RPN is the priority number of one side under the scheme, nil while a value it needs is unknown; risk_rpn in SQL
// works it out alike.
func (sc Scheme) RPN(side requirement.Side) *int {
	if side.Severity == nil || side.Probability == nil {
		return nil
	}
	n := *side.Severity * *side.Probability
	if sc.Detection {
		if side.Detection == nil {
			return nil
		}
		n *= *side.Detection
	}
	return &n
}

// Level is the level one side gives under the scheme, nil for none; risk_level_of in SQL works it out alike.
func (sc Scheme) Level(side requirement.Side) *Level {
	if sc.Method == MethodMatrix {
		if side.Severity == nil || side.Probability == nil {
			return nil
		}
		for _, c := range sc.Matrix {
			if c.Severity == *side.Severity && c.Probability == *side.Probability {
				for i := range sc.Levels {
					if sc.Levels[i].Name == c.Level {
						return &sc.Levels[i]
					}
				}
			}
		}
		return nil
	}
	rpn := sc.RPN(side)
	if rpn == nil {
		return nil
	}
	for i := len(sc.Levels) - 1; i >= 0; i-- {
		if sc.Levels[i].MinRPN == nil || *sc.Levels[i].MinRPN <= *rpn {
			return &sc.Levels[i]
		}
	}
	return nil
}
