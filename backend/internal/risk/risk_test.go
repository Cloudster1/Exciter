package risk

import (
	"errors"
	"fmt"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

func n(v int) *int { return &v }

// fiveByFive is the scheme the settings start from: five values a scale, low, medium and high.
func fiveByFive() SchemeInput {
	in := SchemeInput{Method: MethodMatrix, Levels: []Level{{Name: "Low", Colour: "#16a34a", Acceptable: true}, {Name: "Medium", Colour: "#d97706"}, {Name: "High", Colour: "#dc2626"}}}
	for _, d := range []Dimension{Severity, Probability} {
		sc := ScaleInput{Dimension: d}
		for v := 1; v <= 5; v++ {
			sc.Values = append(sc.Values, ScaleValue{Value: v, Name: fmt.Sprintf("%s %d", d, v)})
		}
		in.Scales = append(in.Scales, sc)
	}
	for s := 1; s <= 5; s++ {
		for p := 1; p <= 5; p++ {
			level := "Low"
			switch {
			case s*p >= 15:
				level = "High"
			case s*p >= 6:
				level = "Medium"
			}
			in.Matrix = append(in.Matrix, MatrixCell{Severity: s, Probability: p, Level: level})
		}
	}
	return in
}

func TestASchemeIsCheckedWhole(t *testing.T) {
	good, err := CheckScheme(fiveByFive())
	if err != nil {
		t.Fatalf("the five by five scheme is refused: %v", err)
	}
	if good.Scales[0].Label != "Severity" || len(good.Matrix) != 25 {
		t.Errorf("the scheme settles as %+v", good)
	}
	refusals := []struct {
		field string
		bend  func(*SchemeInput)
	}{
		{"method", func(in *SchemeInput) { in.Method = "guess" }},
		{"matrix", func(in *SchemeInput) { in.Matrix = in.Matrix[1:] }},
		{"matrix[0].level", func(in *SchemeInput) { in.Matrix[0].Level = "Extreme" }},
		{"scales", func(in *SchemeInput) { in.Scales = in.Scales[:1] }},
		{"scales[2].dimension", func(in *SchemeInput) {
			in.Scales = append(in.Scales, ScaleInput{Dimension: Detection, Values: []ScaleValue{{Value: 1, Name: "Sure"}}})
		}},
		{"scales[0].values[1].value", func(in *SchemeInput) { in.Scales[0].Values[1].Value = 7 }},
		{"scales[0].values[0].name", func(in *SchemeInput) { in.Scales[0].Values[0].Name = " " }},
		{"levels[1].name", func(in *SchemeInput) { in.Levels[1].Name = "low" }},
		{"levels[2].colour", func(in *SchemeInput) { in.Levels[2].Colour = "red" }},
		{"levels", func(in *SchemeInput) { in.Levels = nil }},
	}
	for _, each := range refusals {
		in := fiveByFive()
		each.bend(&in)
		var field *FieldError
		if _, err := CheckScheme(in); !errors.As(err, &field) || field.Field != each.field {
			t.Errorf("bent at %s, the scheme is %v", each.field, err)
		}
	}
}

func TestAPriorityNumberNeedsRisingThresholds(t *testing.T) {
	in := fiveByFive()
	in.Method, in.Detection, in.Matrix = MethodRPN, true, nil
	in.Scales = append(in.Scales, ScaleInput{Dimension: Detection, Values: []ScaleValue{{Value: 1, Name: "Sure"}, {Value: 2, Name: "Likely"}}})
	in.Levels[1].MinRPN, in.Levels[2].MinRPN = n(10), n(5)
	var field *FieldError
	if _, err := CheckScheme(in); !errors.As(err, &field) || field.Field != "levels[2].minRpn" {
		t.Errorf("falling thresholds are %v", err)
	}
	in.Levels[2].MinRPN = n(30)
	got, err := CheckScheme(in)
	if err != nil || got.Levels[0].MinRPN != nil || len(got.Matrix) != 0 {
		t.Errorf("rising thresholds settle as %+v, %v", got, err)
	}
}

func TestALevelFollowsFromTheScheme(t *testing.T) {
	in, _ := CheckScheme(fiveByFive())
	matrix := Scheme{Method: in.Method, Levels: in.Levels, Matrix: in.Matrix}
	for _, each := range []struct {
		side requirement.Side
		want string
	}{
		{requirement.Side{Severity: n(5), Probability: n(4)}, "High"},
		{requirement.Side{Severity: n(2), Probability: n(3)}, "Medium"},
		{requirement.Side{Severity: n(1), Probability: n(1)}, "Low"},
		{requirement.Side{Severity: n(5)}, ""},
	} {
		got := ""
		if l := matrix.Level(each.side); l != nil {
			got = l.Name
		}
		if got != each.want {
			t.Errorf("by the matrix, %+v is %q, want %q", each.side, got, each.want)
		}
	}
	rpn := Scheme{Method: MethodRPN, Detection: true, Levels: []Level{{Name: "Low"}, {Name: "Medium", MinRPN: n(20)}, {Name: "High", MinRPN: n(60)}}}
	for _, each := range []struct {
		side requirement.Side
		want string
		rpn  int
	}{
		{requirement.Side{Severity: n(5), Probability: n(4), Detection: n(3)}, "High", 60},
		{requirement.Side{Severity: n(5), Probability: n(4), Detection: n(1)}, "Medium", 20},
		{requirement.Side{Severity: n(1), Probability: n(1), Detection: n(1)}, "Low", 1},
		{requirement.Side{Severity: n(5), Probability: n(4)}, "", 0},
	} {
		got, number := "", 0
		if l := rpn.Level(each.side); l != nil {
			got = l.Name
		}
		if r := rpn.RPN(each.side); r != nil {
			number = *r
		}
		if got != each.want || number != each.rpn {
			t.Errorf("by priority number, %+v is %q at %d, want %q at %d", each.side, got, number, each.want, each.rpn)
		}
	}
}
