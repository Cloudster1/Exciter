package rql

import (
	"math"
	"strconv"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// keys reads requirement keys, a project's own or one it had, as SQL naming each requirement's id; a key nobody
// holds names none.
func (c *compiler) keys(values []Value) (string, error) {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		if v.Kind != VWord && v.Kind != VString {
			return "", errAt(v.Pos, "%s is not a requirement key. Write it like CHK-R12.", v.String())
		}
		projectKey, number, ok := requirement.ParseKey(v.Text)
		if !ok {
			return "", errAt(v.Pos, "%q is not a requirement key. Write it like CHK-R12.", v.Text)
		}
		parts = append(parts, "requirement_key_id("+c.bind(projectKey)+"::text, "+c.bind(int64(number))+"::int)")
	}
	return strings.Join(parts, ", "), nil
}

// texts reads plain values, normalized, refusing functions and dates.
func (c *compiler) texts(def fieldDef, values []Value, normalize func(string) string) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, v := range values {
		text, err := plainText(def.name, v)
		if err != nil {
			return nil, err
		}
		out = append(out, normalize(text))
	}
	return out, nil
}

// members reads values of an enum, each one of its members.
func (c *compiler) members(def fieldDef, values []Value) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, v := range values {
		m, err := c.member(def, v)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func (c *compiler) member(def fieldDef, v Value) (string, error) {
	text, err := plainText(def.name, v)
	if err != nil {
		return "", err
	}
	text = strings.ToLower(text)
	if !contains(def.enum, text) {
		return "", errAt(v.Pos, "%q is not a %s. Use %s.", v.Text, def.name, describeOperators(def.enum))
	}
	return text, nil
}

func plainText(name string, v Value) (string, error) {
	switch v.Kind {
	case VWord, VString, VNumber:
		return v.Text, nil
	case VFunction:
		return "", errAt(v.Pos, "%s() cannot be compared with %s. Write the value itself.", v.Text, name)
	default:
		return "", errAt(v.Pos, "%q is not a value %s takes.", v.Text, name)
	}
}

func number(name string, v Value) (float64, error) {
	if v.Kind != VNumber {
		return 0, errAt(v.Pos, "%s is a number, and %s is not one.", name, v)
	}
	n, err := strconv.ParseFloat(v.Text, 64)
	if err != nil || math.IsInf(n, 0) || math.IsNaN(n) {
		return 0, errAt(v.Pos, "%q is not a number.", v.Text)
	}
	return n, nil
}

func withParens(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = n + "()"
	}
	return out
}

func anyOf(parts []string) string {
	if len(parts) == 1 {
		return parts[0]
	}
	return "(" + strings.Join(parts, " OR ") + ")"
}

func contains(list []string, s string) bool {
	for _, each := range list {
		if each == s {
			return true
		}
	}
	return false
}
