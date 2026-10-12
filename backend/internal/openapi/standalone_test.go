package openapi

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

type leaf struct {
	Name string `json:"name"`
}

type branch struct {
	Leaves []leaf  `json:"leaves"`
	Parent *branch `json:"parent"`
}

func TestASelfContainedSchemaCarriesWhatItRefersTo(t *testing.T) {
	b := NewBuilder()
	s := b.SelfContained(b.Schema(reflect.TypeOf(branch{})))
	encoded, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if strings.Contains(text, componentsPrefix) {
		t.Errorf("a component reference survived: %s", text)
	}
	if _, ok := s.Defs["Branch"]; !ok {
		t.Errorf("Branch is not carried: %s", text)
	}
	if _, ok := s.Defs["Leaf"]; !ok {
		t.Errorf("Leaf is not carried: %s", text)
	}
	if b.Components()["Branch"].Properties["leaves"].Items.Ref != componentsPrefix+"Leaf" {
		t.Error("the document's own component was rewritten")
	}
}
