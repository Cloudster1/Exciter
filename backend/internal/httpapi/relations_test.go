package httpapi

import (
	"slices"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/armature"
	"github.com/cloudster1/exciter/backend/internal/stator"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// internal/trace sits below the partners' packages and names their relations itself, for the trace model (#151);
// they must stay the partners'.
func TestTheTraceModelKnowsThePartnersRelations(t *testing.T) {
	if !slices.Equal(trace.IssueRelations, enumStrings(armature.Relations)) {
		t.Errorf("the trace model's issue relations are %v, Armature's %v", trace.IssueRelations, armature.Relations)
	}
	if !slices.Equal(trace.PageRelations, enumStrings(stator.Relations)) {
		t.Errorf("the trace model's page relations are %v, Stator's %v", trace.PageRelations, stator.Relations)
	}
}
