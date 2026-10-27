package httpapi

import (
	"net/http"
	"testing"
)

// A refused query tells an assistant where it went wrong, as the grid's caret tells a reader (#311).
func TestARefusedQueryNamesItsCharacterToAnAssistant(t *testing.T) {
	refused := &capture{header: http.Header{}}
	refused.WriteHeader(http.StatusBadRequest)
	_, _ = refused.Write([]byte(`{"error":{"code":"bad_query","message":"Unknown field \"ownr\". Write owner if you mean that one.","position":24}}`))
	want := `Unknown field "ownr". Write owner if you mean that one. The query goes wrong at character 24.`
	if r := refused.toolResult(); !r.IsError || r.StructuredContent != nil || r.Content[0].Text != want {
		t.Errorf("a refused query reads %+v", r)
	}
}
