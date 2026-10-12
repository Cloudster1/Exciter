package openapi_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/openapi"
)

type Stats struct {
	Views int `json:"views"`
}

type report struct {
	Readiness openapi.Stats `json:"readiness"`
	Views     Stats         `json:"views"`
}

func refusal(build func()) (message string) {
	defer func() {
		if r := recover(); r != nil {
			message = fmt.Sprint(r)
		}
	}()
	build()
	return ""
}

func TestTwoTypesWithOneNameAreRefused(t *testing.T) {
	b := openapi.NewBuilder()
	message := refusal(func() { b.Schema(reflect.TypeOf(report{})) })
	if message == "" {
		t.Fatalf("both Stats types were documented without complaint: %v", b.Components()["Stats"])
	}
	for _, want := range []string{
		"github.com/cloudster1/exciter/backend/internal/openapi.Stats",
		"github.com/cloudster1/exciter/backend/internal/openapi_test.Stats",
		"Builder.Names",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("the refusal does not name %s: %s", want, message)
		}
	}
}

func TestANameOverrideKeepsBothTypes(t *testing.T) {
	b := openapi.NewBuilder()
	b.Names[reflect.TypeOf(Stats{})] = "PageViewStats"
	b.FieldOverrides["PageViewStats.views"] = &openapi.Schema{Type: "integer", Description: "Views in all."}
	if message := refusal(func() { b.Schema(reflect.TypeOf(report{})) }); message != "" {
		t.Fatalf("the named pair was refused: %s", message)
	}
	readiness, views := b.Components()["Stats"], b.Components()["PageViewStats"]
	if readiness == nil || readiness.Properties["ready"] == nil {
		t.Errorf("Stats lost its own body: %+v", readiness)
	}
	if views == nil || views.Properties["views"] == nil || views.Properties["views"].Description != "Views in all." {
		t.Errorf("PageViewStats lost its own body or its field override: %+v", views)
	}
	props := b.Components()["Report"].Properties
	if props["readiness"].Ref != "#/components/schemas/Stats" || props["views"].Ref != "#/components/schemas/PageViewStats" {
		t.Errorf("the report refers to the wrong schemas: %+v %+v", props["readiness"], props["views"])
	}
}

func TestANamedTypeStillClashesWithAnotherOfThatName(t *testing.T) {
	b := openapi.NewBuilder()
	b.Names[reflect.TypeOf(Stats{})] = "Stats"
	if refusal(func() { b.Schema(reflect.TypeOf(report{})) }) == "" {
		t.Error("an override that does not tell the two apart was accepted")
	}
}
