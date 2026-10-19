package csvio

import (
	"strconv"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// A risk's rating as spreadsheet cells hold it (#259): one number a cell, before and after mitigation.

// riskTargets are the cells of a rating, each with where its value goes.
var riskTargets = []struct {
	target string
	value  func(*requirement.Rating) **int
}{
	{TargetSeverity, func(r *requirement.Rating) **int { return &r.Before.Severity }},
	{TargetProbability, func(r *requirement.Rating) **int { return &r.Before.Probability }},
	{TargetDetection, func(r *requirement.Rating) **int { return &r.Before.Detection }},
	{TargetSeverityAfter, func(r *requirement.Rating) **int { return &r.After.Severity }},
	{TargetProbabilityAfter, func(r *requirement.Rating) **int { return &r.After.Probability }},
	{TargetDetectionAfter, func(r *requirement.Rating) **int { return &r.After.Detection }},
}

// RiskTargets are the columns of a rating, in the order an export writes them.
var RiskTargets = []string{TargetSeverity, TargetProbability, TargetDetection, TargetSeverityAfter, TargetProbabilityAfter, TargetDetectionAfter}

// ratingCell writes one value of a rating, empty while it is unknown.
func ratingCell(r requirement.Rating, target string) string {
	for _, t := range riskTargets {
		if t.target == target {
			if v := *t.value(&r); v != nil {
				return strconv.Itoa(*v)
			}
		}
	}
	return ""
}

// planRisk reads what a row's cells rate a risk, refusing a rating on a row that is not one and a cell that is no
// whole number.
func (w *world) planRisk(row Row, m Mapping, p *plan, section bool, refuse func(target, format string, args ...any)) {
	var (
		rating requirement.Rating
		first  string
	)
	for _, t := range riskTargets {
		cell := strings.TrimSpace(row.value(m, t.target))
		if cell == "" {
			continue
		}
		if first == "" {
			first = t.target
		}
		v, err := strconv.Atoi(cell)
		if err != nil {
			refuse(t.target, "Line %d rates %s as %q, which is no whole number. Give the value of the scale, such as 3.", row.Line, t.target, cell)
			return
		}
		*t.value(&rating) = &v
	}
	if first == "" {
		return
	}
	typeID := p.TypeID
	if typeID == nil && p.Existing != nil {
		typeID = p.Existing.TypeID
	}
	if typeID == nil && p.Existing == nil {
		typeID = w.project.Settings.DefaultTypeID
	}
	if section || typeID == nil || !w.riskTypes[*typeID] {
		refuse(first, "Line %d rates a risk, which only a requirement of a type marked as risks is. Give it such a type, or clear the cells.", row.Line)
		return
	}
	p.Risk = &rating
}
