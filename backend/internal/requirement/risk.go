package requirement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/db"
)

// A risk is a requirement whose type has the risk role (#259): it carries a rating before and after mitigation
// besides. See docs/api-contract-m13.md.

// Side is a risk's rating on one side of its mitigation: the value of each scale, null while unknown.
type Side struct {
	Severity    *int `json:"severity"`
	Probability *int `json:"probability"`
	// Detection is null while unknown, and always when the project's scheme does not rate it.
	Detection *int `json:"detection"`
}

// Rating is what a risk is rated, before and after it is mitigated.
type Rating struct {
	Before Side `json:"before"`
	After  Side `json:"after"`
}

// RiskLevel is a level of the project's risk scheme, as a side of a rating gives it.
type RiskLevel struct {
	Name       string `json:"name"`
	Colour     string `json:"colour"`
	Acceptable bool   `json:"acceptable"`
}

// RiskState says how far a risk is mitigated (#260): nothing mitigates it, a mitigation is not in effect, or all are.
type RiskState string

const (
	Unmitigated RiskState = "unmitigated"
	Claimed     RiskState = "claimed"
	Mitigated   RiskState = "mitigated"
)

// RiskStates lists every RiskState, for the API document.
var RiskStates = []RiskState{Unmitigated, Claimed, Mitigated}

// RiskView is a risk's rating as it is read: each side with the priority number and the level the scheme gives it.
type RiskView struct {
	Rating
	BeforeRPN   *int       `json:"beforeRpn"`
	AfterRPN    *int       `json:"afterRpn"`
	BeforeLevel *RiskLevel `json:"beforeLevel"`
	AfterLevel  *RiskLevel `json:"afterLevel"`
	// State and Level, the level of the side in effect, arrive with mitigations (#260); a revision, read without
	// its requirement, has neither.
	State *RiskState `json:"state"`
	Level *RiskLevel `json:"level"`
}

// RiskMark is how the trace graph and the chain report mark a risk (#261).
type RiskMark struct {
	Level *RiskLevel `json:"level"`
	State RiskState  `json:"state"`
}

// MaxRiskValue is the highest value of any scale (#258), as internal/risk agrees it.
const MaxRiskValue = 10

// RoleRisk is the role of a type whose requirements are risks.
const RoleRisk = attribute.RoleRisk

// Empty says whether a side gives no value.
func (s Side) Empty() bool { return s.Severity == nil && s.Probability == nil && s.Detection == nil }

// Empty says whether a rating gives no value on either side.
func (r Rating) Empty() bool { return r.Before.Empty() && r.After.Empty() }

// column is the rating as the database keeps it, null when it gives nothing.
func (r Rating) column() (any, error) {
	if r.Empty() {
		return nil, nil
	}
	return json.Marshal(r)
}

// scanRating reads the column a select gave.
func scanRating(raw []byte) (Rating, error) {
	var r Rating
	if len(raw) == 0 || string(raw) == "null" {
		return r, nil
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return r, fmt.Errorf("read the rating: %w", err)
	}
	return r, nil
}

// ReadRating reads a rating as the database keeps it, for whoever selects its column elsewhere.
func ReadRating(raw []byte) (Rating, error) { return scanRating(raw) }

func (s Side) hashed() map[string]any {
	out := map[string]any{}
	for name, v := range map[string]*int{"severity": s.Severity, "probability": s.Probability, "detection": s.Detection} {
		if v != nil {
			out[name] = *v
		}
	}
	return out
}

// hashed adds the rating to the content a hash covers, and nothing when it gives nothing, so every other requirement
// keeps the hash it had.
func (r Rating) hashed(content map[string]any) {
	if !r.Empty() {
		content["risk"] = map[string]any{"before": r.Before.hashed(), "after": r.After.hashed()}
	}
}

// checkRating refuses a value no scale can hold, naming the field.
func checkRating(in Rating) error {
	for _, side := range []struct {
		name string
		side Side
	}{{"before", in.Before}, {"after", in.After}} {
		for _, v := range []struct {
			name  string
			value *int
		}{{"severity", side.side.Severity}, {"probability", side.side.Probability}, {"detection", side.side.Detection}} {
			if v.value != nil && (*v.value < 1 || *v.value > MaxRiskValue) {
				return &FieldError{"risk." + side.name + "." + v.name, fmt.Sprintf("A value is from 1 to %d. Pick one of the project's scale.", MaxRiskValue)}
			}
		}
	}
	return nil
}

var (
	// errNotRisk refuses a rating on anything but a risk.
	errNotRisk = &FieldError{"risk", "Only a requirement of a type marked as risks is rated. Pick such a type first."}
	// errNoScheme refuses a rating in a project whose administrators set no scheme yet.
	errNoScheme = &FieldError{"risk", "This project has no risk scheme yet. Ask one of its administrators to set one under Settings, Risks."}
	// errOffScale refuses a value the project's scales lack.
	errOffScale = &FieldError{"risk", "The rating uses a value the project's scales lack. Pick values from its risk scheme."}
	// errMitigationHeld refuses a type that would leave a mitigation joining two risks, or two requirements (#325).
	errMitigationHeld = &FieldError{"typeId", "A mitigation joins this requirement to a risk, which only a requirement that is no risk mitigates. Remove the link first, or keep a type of the same role."}
)

// ErrRatingsDoNotFit refuses moving a rated risk to a project whose scales lack its values.
var ErrRatingsDoNotFit = errors.New("the rating uses values the other project's scales lack")

// ratingFor keeps a rating when its type is a risk type and drops it otherwise, as a change of type does.
func ratingFor(ctx context.Context, tx db.DBTX, typeID *uuid.UUID, r Rating) (Rating, error) {
	if r.Empty() {
		return Rating{}, nil
	}
	role, err := roleOf(ctx, tx, typeID)
	if err != nil || role != RoleRisk {
		return Rating{}, err
	}
	return r, nil
}

// ratingFits says whether a project's scales hold every value of a rating, and whether it has a scheme at all.
func ratingFits(ctx context.Context, tx db.DBTX, projectID uuid.UUID, r Rating) (fits, scheme bool, err error) {
	encoded, err := json.Marshal(r)
	if err != nil {
		return false, false, err
	}
	err = tx.QueryRow(ctx, `SELECT risk_rating_fits($1, $2), EXISTS (SELECT 1 FROM risk_scheme WHERE project_id = $1)`,
		projectID, encoded).Scan(&fits, &scheme)
	return fits, scheme, err
}

// ratingOf checks what a writer sent as the rating of a requirement of typeID in a project; nil sends nothing.
func ratingOf(ctx context.Context, tx db.DBTX, projectID uuid.UUID, typeID *uuid.UUID, in *Rating) (Rating, error) {
	if in == nil || in.Empty() {
		return Rating{}, nil
	}
	if err := checkRating(*in); err != nil {
		return Rating{}, err
	}
	role, err := roleOf(ctx, tx, typeID)
	if err != nil {
		return Rating{}, err
	}
	if role != RoleRisk {
		return Rating{}, errNotRisk
	}
	fits, scheme, err := ratingFits(ctx, tx, projectID, *in)
	switch {
	case err != nil:
		return Rating{}, err
	case !scheme:
		return Rating{}, errNoScheme
	case !fits:
		return Rating{}, errOffScale
	}
	return *in, nil
}

// fitRating keeps what of a rating the scales of another project hold, as a copy or a reuse takes it there.
func fitRating(ctx context.Context, tx db.DBTX, projectID uuid.UUID, r Rating) (Rating, error) {
	if r.Empty() {
		return r, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT l.dimension, l.value FROM risk_scale_level l JOIN risk_scheme s ON s.project_id = l.project_id
		WHERE l.project_id = $1 AND (l.dimension <> 'detection' OR s.detection)`, projectID)
	if err != nil {
		return r, err
	}
	held := map[string]bool{}
	for rows.Next() {
		var (
			dimension string
			value     int
		)
		if err := rows.Scan(&dimension, &value); err != nil {
			rows.Close()
			return r, err
		}
		held[fmt.Sprintf("%s %d", dimension, value)] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return r, err
	}
	keep := func(dimension string, v *int) *int {
		if v != nil && held[fmt.Sprintf("%s %d", dimension, *v)] {
			return v
		}
		return nil
	}
	fit := func(s Side) Side {
		return Side{Severity: keep("severity", s.Severity), Probability: keep("probability", s.Probability), Detection: keep("detection", s.Detection)}
	}
	return Rating{Before: fit(r.Before), After: fit(r.After)}, nil
}

// ratingRefusal says what the database refused of a rating.
func ratingRefusal(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.ConstraintName {
		case "requirement_risk_fields":
			return errNotRisk
		case "requirement_risk_scale":
			return errOffScale
		case "requirement_mitigation_held":
			return errMitigationHeld
		}
	}
	return err
}

// riskView reads a rating with what risk_view worked out of it.
func riskView(r Rating, view []byte) (*RiskView, error) {
	out := RiskView{}
	if len(view) > 0 {
		if err := json.Unmarshal(view, &out); err != nil {
			return nil, fmt.Errorf("read the rating's levels: %w", err)
		}
	}
	out.Rating = r
	return &out, nil
}

// RiskDiff is one value of a risk's rating that changed between two revisions; null where it gave none.
type RiskDiff struct {
	Side      string `json:"side"`
	Dimension string `json:"dimension"`
	From      *int   `json:"from"`
	To        *int   `json:"to"`
}

// riskDiff lists the values that changed, before first, in the order of the scales.
func riskDiff(a, b Rating) []RiskDiff {
	out := []RiskDiff{}
	for _, side := range []struct {
		name string
		a, b Side
	}{{"before", a.Before, b.Before}, {"after", a.After, b.After}} {
		for _, v := range []struct {
			name string
			a, b *int
		}{{"severity", side.a.Severity, side.b.Severity}, {"probability", side.a.Probability, side.b.Probability},
			{"detection", side.a.Detection, side.b.Detection}} {
			if (v.a == nil) != (v.b == nil) || (v.a != nil && *v.a != *v.b) {
				out = append(out, RiskDiff{Side: side.name, Dimension: v.name, From: v.a, To: v.b})
			}
		}
	}
	return out
}

// RatingText writes a rating as one line of text, as ReqIF keeps it: "before 4 2 -; after 2 2 -", severity,
// probability and detection, a dash for a value unknown.
func RatingText(r Rating) string {
	side := func(s Side) string {
		out := ""
		for i, v := range []*int{s.Severity, s.Probability, s.Detection} {
			if i > 0 {
				out += " "
			}
			if v == nil {
				out += "-"
			} else {
				out += fmt.Sprint(*v)
			}
		}
		return out
	}
	return "before " + side(r.Before) + "; after " + side(r.After)
}

// RatingOf reads a rating back from RatingText; a part it cannot read refuses the whole.
func RatingOf(text string) (Rating, error) {
	var r Rating
	for _, part := range strings.Split(text, ";") {
		words := strings.Fields(part)
		if len(words) == 0 {
			continue
		}
		var side *Side
		switch strings.ToLower(words[0]) {
		case "before":
			side = &r.Before
		case "after":
			side = &r.After
		default:
			return Rating{}, fmt.Errorf("a rating names before or after, not %q", words[0])
		}
		if len(words) != 4 {
			return Rating{}, fmt.Errorf("a side of a rating gives severity, probability and detection, not %q", part)
		}
		for i, slot := range []**int{&side.Severity, &side.Probability, &side.Detection} {
			if words[i+1] == "-" {
				continue
			}
			v, err := strconv.Atoi(words[i+1])
			if err != nil {
				return Rating{}, fmt.Errorf("a value of a rating is a whole number, not %q", words[i+1])
			}
			*slot = &v
		}
	}
	return r, nil
}

// ReadRiskView reads a rating and what risk_view worked out of it, for whoever selects both elsewhere; nil when the
// rating gives nothing.
func ReadRiskView(rating, view []byte) (*RiskView, error) {
	r, err := scanRating(rating)
	if err != nil || r.Empty() {
		return nil, err
	}
	return riskView(r, view)
}
