package requirement

import (
	"encoding/json"
	"slices"
	"strings"
)

// DiffMoved is a step that says the same at another place.
const DiffMoved DiffChange = "moved"

// StepDiff is one step in a comparison: where it was and is, and its words compared.
type StepDiff struct {
	Change DiffChange `json:"change"`
	// From and To are its numbers on each side, from 1; null on the side it is missing from.
	From     *int        `json:"from"`
	To       *int        `json:"to"`
	Action   []DiffBlock `json:"action"`
	Expected []DiffBlock `json:"expected"`
}

// ParameterDiff is one parameter in a comparison, its values on each side.
type ParameterDiff struct {
	Name   string     `json:"name"`
	Change DiffChange `json:"change"`
	From   []string   `json:"from"`
	To     []string   `json:"to"`
}

// stepKey is a step as compared: its words with white space settled.
func stepKey(s Step) string {
	encoded, _ := json.Marshal(map[string]any{"action": canonical(s.Action), "expected": canonical(s.Expected)})
	return string(encoded)
}

// commonSteps pairs the longest run of steps alike in the same order on both sides, as index pairs.
func commonSteps(a, b []string) [][2]int {
	n, m := len(a), len(b)
	length := make([][]int, n+1)
	for i := range length {
		length[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				length[i][j] = length[i+1][j+1] + 1
			} else {
				length[i][j] = max(length[i+1][j], length[i][j+1])
			}
		}
	}
	var out [][2]int
	for i, j := 0, 0; i < n && j < m; {
		switch {
		case a[i] == b[j]:
			out = append(out, [2]int{i, j})
			i, j = i+1, j+1
		case length[i+1][j] >= length[i][j+1]:
			i++
		default:
			j++
		}
	}
	return out
}

// sideDiff compares one step's two sides, an empty step standing for the side it is missing from.
func sideDiff(change DiffChange, from, to Step, fromNumber, toNumber *int) (StepDiff, error) {
	d := StepDiff{Change: change, From: fromNumber, To: toNumber}
	var err error
	if d.Action, err = Diff(orEmpty(from.Action), orEmpty(to.Action)); err != nil {
		return d, err
	}
	d.Expected, err = Diff(orEmpty(from.Expected), orEmpty(to.Expected))
	return d, err
}

// stepDiff compares two lists of steps: alike in order is equal, alike elsewhere moved, the rest paired in order as
// changed or else added or removed; in the newer side's order, a removed step after the one it followed.
func stepDiff(from, to []Step) ([]StepDiff, error) {
	a, b := make([]string, len(from)), make([]string, len(to))
	for i, s := range from {
		a[i] = stepKey(s)
	}
	for j, s := range to {
		b[j] = stepKey(s)
	}
	pairOf, change, taken := map[int]int{}, map[int]DiffChange{}, map[int]bool{}
	for _, m := range commonSteps(a, b) {
		pairOf[m[0]], change[m[0]], taken[m[1]] = m[1], DiffEqual, true
	}
	var unmatchedA []int
	for i := range a {
		if _, ok := pairOf[i]; ok {
			continue
		}
		moved := -1
		for j := range b {
			if !taken[j] && b[j] == a[i] {
				moved = j
				break
			}
		}
		if moved >= 0 {
			pairOf[i], change[i], taken[moved] = moved, DiffMoved, true
			continue
		}
		unmatchedA = append(unmatchedA, i)
	}
	var unmatchedB []int
	for j := range b {
		if !taken[j] {
			unmatchedB = append(unmatchedB, j)
		}
	}
	for n := 0; n < len(unmatchedA) && n < len(unmatchedB); n++ {
		pairOf[unmatchedA[n]], change[unmatchedA[n]], taken[unmatchedB[n]] = unmatchedB[n], DiffModified, true
	}
	// Each step's place in the newer order, doubled so a removed step sits between two kept ones.
	type placed struct {
		at   int
		diff StepDiff
	}
	var out []placed
	for i := range from {
		was := i + 1
		j, paired := pairOf[i]
		if !paired {
			d, err := sideDiff(DiffDeleted, from[i], Step{}, &was, nil)
			if err != nil {
				return nil, err
			}
			at := -1
			for k := i - 1; k >= 0; k-- {
				if before, ok := pairOf[k]; ok {
					at = 2*before + 1
					break
				}
			}
			out = append(out, placed{at, d})
			continue
		}
		is := j + 1
		d, err := sideDiff(change[i], from[i], to[j], &was, &is)
		if err != nil {
			return nil, err
		}
		out = append(out, placed{2 * j, d})
	}
	for j := range to {
		if taken[j] {
			continue
		}
		is := j + 1
		d, err := sideDiff(DiffInserted, Step{}, to[j], nil, &is)
		if err != nil {
			return nil, err
		}
		out = append(out, placed{2 * j, d})
	}
	slices.SortStableFunc(out, func(x, y placed) int { return x.at - y.at })
	diffs := make([]StepDiff, len(out))
	for i, p := range out {
		diffs[i] = p.diff
	}
	return diffs, nil
}

// parameterDiff lists each parameter on either side by name, in the newer side's order, then those removed.
func parameterDiff(from, to []Parameter) []ParameterDiff {
	out := []ParameterDiff{}
	had := map[string]Parameter{}
	for _, p := range from {
		had[strings.ToLower(p.Name)] = p
	}
	seen := map[string]bool{}
	for _, p := range to {
		key := strings.ToLower(p.Name)
		seen[key] = true
		old, ok := had[key]
		d := ParameterDiff{Name: p.Name, Change: DiffEqual, From: old.Values, To: p.Values}
		switch {
		case !ok:
			d.Change, d.From = DiffInserted, []string{}
		case !slices.Equal(old.Values, p.Values) || old.Name != p.Name:
			d.Change = DiffModified
		}
		out = append(out, d)
	}
	for _, p := range from {
		if !seen[strings.ToLower(p.Name)] {
			out = append(out, ParameterDiff{Name: p.Name, Change: DiffDeleted, From: p.Values, To: []string{}})
		}
	}
	return out
}
