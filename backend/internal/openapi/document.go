// Package openapi builds an OpenAPI 3.1 document from the server's own Go
// types, so the schemas cannot drift from what it sends, and checks values against it.
package openapi

import (
	"encoding/json"
	"reflect"
	"sort"
)

// Document is the root of the specification.
type Document struct {
	OpenAPI    string                `json:"openapi"`
	Info       Info                  `json:"info"`
	Servers    []Server              `json:"servers,omitempty"`
	Tags       []Tag                 `json:"tags,omitempty"`
	Paths      map[string]PathItem   `json:"paths"`
	Components Components            `json:"components"`
	Security   []map[string][]string `json:"security,omitempty"`
}

type Info struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

type Server struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

type Tag struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// PathItem maps a lower case method to its operation.
type PathItem map[string]*Operation

type Operation struct {
	OperationID string                `json:"operationId"`
	Summary     string                `json:"summary,omitempty"`
	Description string                `json:"description,omitempty"`
	Tags        []string              `json:"tags,omitempty"`
	Parameters  []Parameter           `json:"parameters,omitempty"`
	RequestBody *RequestBody          `json:"requestBody,omitempty"`
	Responses   map[string]*Response  `json:"responses"`
	Security    []map[string][]string `json:"security,omitempty"`
}

type Parameter struct {
	Name        string  `json:"name"`
	In          string  `json:"in"`
	Required    bool    `json:"required,omitempty"`
	Description string  `json:"description,omitempty"`
	Schema      *Schema `json:"schema"`
}

type RequestBody struct {
	Description string               `json:"description,omitempty"`
	Required    bool                 `json:"required,omitempty"`
	Content     map[string]MediaType `json:"content"`
}

type MediaType struct {
	Schema *Schema `json:"schema,omitempty"`
}

type Response struct {
	Description string               `json:"description"`
	Content     map[string]MediaType `json:"content,omitempty"`
}

type Components struct {
	Schemas         map[string]*Schema        `json:"schemas"`
	SecuritySchemes map[string]SecurityScheme `json:"securitySchemes,omitempty"`
}

type SecurityScheme struct {
	Type   string `json:"type"`
	Scheme string `json:"scheme,omitempty"`
	// BearerFormat hints at what a bearer token looks like.
	BearerFormat string `json:"bearerFormat,omitempty"`
	In           string `json:"in,omitempty"`
	Name         string `json:"name,omitempty"`
	Description  string `json:"description,omitempty"`
}

// Schema is the subset of JSON Schema the builder produces. Type is a string
// or, for a nullable value, a list ending in "null", which is how 3.1 says it.
type Schema struct {
	Ref                  string             `json:"$ref,omitempty"`
	Type                 any                `json:"type,omitempty"`
	Format               string             `json:"format,omitempty"`
	Description          string             `json:"description,omitempty"`
	Enum                 []string           `json:"enum,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	Required             []string           `json:"required,omitempty"`
	Items                *Schema            `json:"items,omitempty"`
	AdditionalProperties *Schema            `json:"additionalProperties,omitempty"`
	OneOf                []*Schema          `json:"oneOf,omitempty"`
	// Defs carries the components a schema refers to when it has to stand
	// alone, outside the document; the document itself never sets it.
	Defs map[string]*Schema `json:"$defs,omitempty"`
}

// Nullable returns a schema that also accepts null.
func Nullable(s *Schema) *Schema {
	if s == nil {
		return &Schema{Type: "null"}
	}
	if s.Ref != "" || len(s.OneOf) > 0 {
		return &Schema{OneOf: []*Schema{s, {Type: "null"}}}
	}
	copied := *s
	switch t := s.Type.(type) {
	case string:
		if t == "" {
			return &copied
		}
		copied.Type = []string{t, "null"}
	case []string:
		for _, each := range t {
			if each == "null" {
				return &copied
			}
		}
		copied.Type = append(append([]string(nil), t...), "null")
	}
	return &copied
}

// Types lists what a schema's type allows, one entry per accepted type.
func (s *Schema) Types() []string {
	switch t := s.Type.(type) {
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	case []string:
		return t
	}
	return nil
}

// MarshalIndent renders the document with stable key order, so a checked in
// copy only changes when the API does.
func (d *Document) MarshalIndent() ([]byte, error) {
	return json.MarshalIndent(d, "", "  ")
}

// SortedKeys is the deterministic order maps are walked in.
func SortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// isNil reports whether a reflect value is a nil pointer, map, slice or interface.
func isNil(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface:
		return v.IsNil()
	}
	return false
}
