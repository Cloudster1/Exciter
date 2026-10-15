package reqifio

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

func bufioWriter(w io.Writer) *bufio.Writer { return bufio.NewWriter(w) }

// sample is a file with every kind of element and value, as an export makes one.
func sample() *File {
	at := time.Date(2026, 10, 1, 12, 30, 0, 0, time.UTC)
	id := func(identifier, name string) Identity {
		return Identity{Identifier: identifier, Name: name, Changed: at}
	}
	statement := FromDocument(json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Brakes & <wheels> \"stop\""}]}]}`), nil)
	return &File{
		Identifier: "exciter-export-1", Title: "Brakes", Comment: "From Exciter", SourceTool: "Exciter", ToolID: "Exciter 1.0", Created: at,
		DataTypes: []*DataType{
			{Identity: id("dt-s", "Text"), Kind: KindString, MaxLength: 1000},
			{Identity: id("dt-x", "Rich text"), Kind: KindXHTML},
			{Identity: id("dt-i", "Whole number"), Kind: KindInteger, Min: "0", Max: "10"},
			{Identity: id("dt-r", "Number"), Kind: KindReal, Accuracy: 10, Min: "-1e308", Max: "1e308"},
			{Identity: id("dt-b", "Yes or no"), Kind: KindBoolean},
			{Identity: id("dt-d", "Date"), Kind: KindDate},
			{Identity: id("dt-e", "Risk"), Kind: KindEnum, Values: []EnumValue{{Identity: id("ev-1", "High"), Key: 0}, {Identity: id("ev-2", "Low"), Key: 1, Other: "x"}}},
		},
		Types: []*SpecType{
			{Identity: id("t-req", "Functional"), Kind: TypeObject, Attributes: []*AttrDef{
				{Identity: id("a-name", "ReqIF.Name"), Kind: KindString, DataType: "dt-s"},
				{Identity: id("a-text", "ReqIF.Text"), Kind: KindXHTML, DataType: "dt-x"},
				{Identity: id("a-count", "Count"), Kind: KindInteger, DataType: "dt-i"},
				{Identity: id("a-weight", "Weight"), Kind: KindReal, DataType: "dt-r"},
				{Identity: id("a-safe", "Safe"), Kind: KindBoolean, DataType: "dt-b"},
				{Identity: id("a-due", "Due"), Kind: KindDate, DataType: "dt-d"},
				{Identity: id("a-risk", "Risk"), Kind: KindEnum, DataType: "dt-e", Multi: true},
			}},
			{Identity: id("t-spec", "Specification"), Kind: TypeSpecification},
			{Identity: id("t-rel", "derives"), Kind: TypeRelation},
		},
		Objects: []*Object{
			{Identity: id("o-1", ""), Type: "t-req", Values: []Value{
				{Def: "a-name", Text: "Stop & go"},
				{Def: "a-text", XHTML: statement},
				{Def: "a-count", Text: "3"},
				{Def: "a-weight", Text: "2.5"},
				{Def: "a-safe", Text: "true"},
				{Def: "a-due", Text: "2026-12-01T00:00:00.000Z"},
				{Def: "a-risk", Enum: []string{"ev-1", "ev-2"}},
			}},
			{Identity: id("o-2", "Second"), Type: "t-req"},
		},
		Relations:      []*Relation{{Identity: id("r-1", ""), Type: "t-rel", Source: "o-2", Target: "o-1"}},
		Specifications: []*Specification{{Identity: id("s-1", "Brakes"), Type: "t-spec", Children: []*Node{{Identity: id("h-1", ""), Object: "o-1", Children: []*Node{{Identity: id("h-2", ""), Object: "o-2"}}}}}},
	}
}

func TestWriteThenReadGivesTheSameFile(t *testing.T) {
	want := sample()
	var buf bytes.Buffer
	if err := Write(&buf, want); err != nil {
		t.Fatal(err)
	}
	got, _, _, err := Read(buf.Bytes())
	if err != nil {
		t.Fatalf("%v\n%s", err, buf.String())
	}
	// Compared as an import keeps it, so a time read back in UTC equals the one written.
	g, _ := json.MarshalIndent(got, "", " ")
	w, _ := json.MarshalIndent(want, "", " ")
	if string(g) != string(w) {
		t.Errorf("read back\n%s\nwant\n%s", g, w)
	}
}

func TestWriteEscapesWhatItWrites(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, sample()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{`THE-VALUE="Stop &amp; go"`, `Brakes &amp; &lt;wheels&gt; &#34;stop&#34;`, `<xhtml:div><xhtml:p>`, `MULTI-VALUED="true"`} {
		if !strings.Contains(out, want) {
			t.Errorf("the document lacks %s", want)
		}
	}
	if strings.Contains(out, "<wheels>") {
		t.Errorf("text went out unescaped")
	}
}
