package requirement

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/document"
)

// StepsTable is a test case's steps as a document holding one table: a header row naming the columns, then a row
// per step, which Markdown, Word and ReqIF write as any table.
func StepsTable(steps []Step, action, expected string) json.RawMessage {
	rows := []document.Node{{Type: "tableRow", Content: []document.Node{headerCell(action), headerCell(expected)}}}
	for _, s := range steps {
		rows = append(rows, document.Node{Type: "tableRow", Content: []document.Node{cellOf(s.Action), cellOf(s.Expected)}})
	}
	return tableDoc(rows)
}

// ParametersTable is a test case's parameters as a table of iterations: their names, then a row of values each.
func ParametersTable(t TestCase, iteration string) json.RawMessage {
	header := []document.Node{headerCell(iteration)}
	for _, p := range t.Parameters {
		header = append(header, headerCell(p.Name))
	}
	rows := []document.Node{{Type: "tableRow", Content: header}}
	for n := 1; n <= t.Iterations(); n++ {
		row := []document.Node{textCell("tableCell", strconv.Itoa(n))}
		for _, p := range t.Parameters {
			row = append(row, textCell("tableCell", p.Values[n-1]))
		}
		rows = append(rows, document.Node{Type: "tableRow", Content: row})
	}
	return tableDoc(rows)
}

// StepsFromTable reads steps back from a document whose first table has an action and an expected result a row,
// under a header row; nil when it holds no table.
func StepsFromTable(raw json.RawMessage) ([]Step, error) {
	root, err := document.Parse(raw)
	if err != nil {
		return nil, err
	}
	table := firstTable(root)
	if table == nil {
		return nil, nil
	}
	steps := []Step{}
	for i, row := range table.Content {
		if i == 0 {
			continue
		}
		var s Step
		if len(row.Content) > 0 {
			if s.Action, err = cellDoc(row.Content[0]); err != nil {
				return nil, err
			}
		}
		if len(row.Content) > 1 {
			if s.Expected, err = cellDoc(row.Content[1]); err != nil {
				return nil, err
			}
		}
		steps = append(steps, s)
	}
	return steps, nil
}

func firstTable(n document.Node) *document.Node {
	if n.Type == "table" {
		return &n
	}
	for _, c := range n.Content {
		if t := firstTable(c); t != nil {
			return t
		}
	}
	return nil
}

func tableDoc(rows []document.Node) json.RawMessage {
	raw, _ := json.Marshal(document.Node{Type: "doc", Content: []document.Node{{Type: "table", Content: rows}}})
	return raw
}

func headerCell(text string) document.Node { return textCell("tableHeader", text) }

func textCell(kind, text string) document.Node {
	paragraph := document.Node{Type: "paragraph"}
	if text != "" {
		paragraph.Content = []document.Node{{Type: "text", Text: text}}
	}
	return document.Node{Type: kind, Content: []document.Node{paragraph}}
}

// cellOf is a cell holding a document's blocks, an empty paragraph for none.
func cellOf(raw json.RawMessage) document.Node {
	cell := document.Node{Type: "tableCell", Content: []document.Node{{Type: "paragraph"}}}
	if len(raw) == 0 || isNull(raw) {
		return cell
	}
	var doc document.Node
	if json.Unmarshal(raw, &doc) == nil && len(doc.Content) > 0 {
		cell.Content = doc.Content
	}
	return cell
}

// cellDoc is a cell's blocks as a document, nil when they hold no words.
func cellDoc(cell document.Node) (json.RawMessage, error) {
	raw, err := json.Marshal(document.Node{Type: "doc", Content: cell.Content})
	if err != nil || blank(raw) {
		return nil, err
	}
	return raw, nil
}

// ParametersText writes each parameter on a line of its own: its name, a colon, and its values apart by semicolons.
func ParametersText(params []Parameter) string {
	lines := make([]string, len(params))
	for i, p := range params {
		lines[i] = p.Name + ": " + strings.Join(p.Values, "; ")
	}
	return strings.Join(lines, "\n")
}

// ParametersOf reads parameters back; a line without a colon is a name without values, which the save refuses.
func ParametersOf(text string) []Parameter {
	var out []Parameter
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		name, values, _ := strings.Cut(line, ":")
		p := Parameter{Name: strings.TrimSpace(name), Values: []string{}}
		for _, v := range strings.Split(values, ";") {
			if v = strings.TrimSpace(v); v != "" {
				p.Values = append(p.Values, v)
			}
		}
		out = append(out, p)
	}
	return out
}

// RatingWords are the headings a rating's table is written with, in the reader's language.
type RatingWords struct {
	Rating, Before, After, Severity, Probability, Detection, RPN, Level string
}

// RatingTable is a risk's rating as a table (#259): a row each for before and after mitigation, a column a value,
// then the priority number and the level; detection only when a side gives it.
func RatingTable(v RiskView, words RatingWords) json.RawMessage {
	detection := v.Before.Detection != nil || v.After.Detection != nil
	header := []document.Node{headerCell(words.Rating), headerCell(words.Severity), headerCell(words.Probability)}
	if detection {
		header = append(header, headerCell(words.Detection))
	}
	header = append(header, headerCell(words.RPN), headerCell(words.Level))
	rows := []document.Node{{Type: "tableRow", Content: header}}
	number := func(n *int) string {
		if n == nil {
			return "-"
		}
		return strconv.Itoa(*n)
	}
	for _, side := range []struct {
		name  string
		side  Side
		rpn   *int
		level *RiskLevel
	}{{words.Before, v.Before, v.BeforeRPN, v.BeforeLevel}, {words.After, v.After, v.AfterRPN, v.AfterLevel}} {
		row := []document.Node{textCell("tableCell", side.name), textCell("tableCell", number(side.side.Severity)), textCell("tableCell", number(side.side.Probability))}
		if detection {
			row = append(row, textCell("tableCell", number(side.side.Detection)))
		}
		level := "-"
		if side.level != nil {
			level = side.level.Name
		}
		row = append(row, textCell("tableCell", number(side.rpn)), textCell("tableCell", level))
		rows = append(rows, document.Node{Type: "tableRow", Content: row})
	}
	return tableDoc(rows)
}

// EnglishRatingWords head a rating's table where no language is chosen, as in Markdown.
var EnglishRatingWords = RatingWords{Rating: "Rating", Before: "Before", After: "After", Severity: "Severity", Probability: "Probability",
	Detection: "Detection", RPN: "RPN", Level: "Level"}
