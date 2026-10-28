package httpapi

import (
	"net/http"
	"testing"
)

// A refused tool call names each field it got wrong with the sentence a form shows beside it, in the fields' order,
// so an assistant can mend its call (#324).
func TestARefusalNamesEachFieldToAnAssistant(t *testing.T) {
	refused := &capture{header: http.Header{}}
	refused.WriteHeader(http.StatusUnprocessableEntity)
	_, _ = refused.Write([]byte(`{"error":{"code":"validation_failed","message":"Some fields need attention.",` +
		`"fields":{"steps[1]":"The case has steps 1 to 1 at the revision it runs. Record one of those.","outcome":"Record passed, failed, blocked or skipped."}}}`))
	want := "Some fields need attention. outcome: Record passed, failed, blocked or skipped. " +
		"steps[1]: The case has steps 1 to 1 at the revision it runs. Record one of those."
	if r := refused.toolResult(); !r.IsError || r.StructuredContent != nil || r.Content[0].Text != want {
		t.Errorf("a refusal on fields reads %+v", r)
	}
}
