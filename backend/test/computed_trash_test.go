//go:build integration

package test

import (
	"net/http"
	"testing"
)

// A section's sum over its children (#319) leaves out a child in the trash and counts it again once it is restored.
func TestASumOverChildrenFollowsAChildToTheTrashAndBack(t *testing.T) {
	s := newComputedScene(t, "CTR")
	section, force := s.sectionA["key"].(string), s.key+"-R5"
	forceID := obj(t, want(t, s.owner.get(t, "/api/v1/requirements/"+force), http.StatusOK, "read R5"), "requirement")["id"].(string)
	eventually(t, s.carol, section, s.total, "15")

	want(t, s.owner.delete(t, "/api/v1/requirements/"+force), http.StatusNoContent, "trash R5")
	eventually(t, s.carol, section, s.total, "10")

	want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/trash/"+forceID+"/restore", nil), http.StatusOK, "restore R5")
	eventually(t, s.carol, section, s.total, "15")
	restored := obj(t, want(t, s.carol.get(t, "/api/v1/requirements/"+force), http.StatusOK, "read R5 again"), "requirement")
	if restored["parentId"] != s.sectionA["id"] {
		t.Errorf("R5 came back under %v, want the section it left", restored["parentId"])
	}
}
