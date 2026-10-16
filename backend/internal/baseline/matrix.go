package baseline

import (
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// A baseline's traceability matrix and what changed in it since (#153), agreed in docs/api-contract-m5.md.

// Detail says how much a baseline recorded: DetailLinks only its links, DetailFull also types, suspect state, Armature
// status and Stator links.
const (
	DetailLinks = 1
	DetailFull  = 2
)

// MatrixLink is one link of a cell as the baseline holds it; Suspect and Status are null where it did not record them.
type MatrixLink struct {
	Kind      trace.LinkKind `json:"kind"`
	Name      string         `json:"name"`
	TypeKey   string         `json:"typeKey,omitempty"`
	Direction string         `json:"direction,omitempty"`
	Suspect   *bool          `json:"suspect"`
	Status    *string        `json:"status"`
	// Change is set only when compared: what the other side has instead.
	Change Change `json:"change,omitempty"`
}

// MatrixCell is every link between one row and one column of a baseline's matrix.
type MatrixCell struct {
	Row    int          `json:"row"`
	Column int          `json:"column"`
	Links  []MatrixLink `json:"links"`
}

// Matrix is a page of a baseline's requirements against what they linked to, and against With when compared.
type Matrix struct {
	Detail  int                  `json:"detail"`
	With    *Side                `json:"with"`
	Rows    []requirement.Ref    `json:"rows"`
	Columns []trace.MatrixColumn `json:"columns"`
	Cells   []MatrixCell         `json:"cells"`
	Total   int                  `json:"total"`
	Limit   int                  `json:"limit"`
	Offset  int                  `json:"offset"`
}
