//go:build integration

package test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"
)

// Roles, the matrix, groups and grants across the installation are the administrators' to change, each answered
// and each refused for its reason (#471).
func TestAccessOverTheAPI(t *testing.T) {
	h := newHarness(t)
	h.keepRoles(t)
	a := newAPIServer(t, h)
	home := h.owner(t, "access")
	memberID, otherID, guestID := h.namedPerson(t, "Mia Member"), h.addPerson(t, "member"), h.addPerson(t, "guest")
	admin, member, nobody := a.as(t, home.user), a.as(t, memberID), a.anonymous()
	roleKey := "auditor_" + h.suffix()

	t.Run("every member reads the roles and the matrix's permissions", func(t *testing.T) {
		roles := list(t, want(t, member.get(t, "/api/v1/roles"), http.StatusOK, "list the roles"), "roles")
		var keys []string
		for _, r := range roles {
			keys = append(keys, r.(map[string]any)["role"].(string))
		}
		if len(keys) < 5 || !slices.Equal(keys[:5], []string{"administrator", "admin", "editor", "reviewer", "viewer"}) {
			t.Errorf("the roles start %v, want the built-in ones first", keys)
		}
		administrator := roles[0].(map[string]any)
		if administrator["builtin"] != true || administrator["orgWideOnly"] != true || !slices.Contains(administrator["permissions"].([]any), any("administerInstallation")) {
			t.Errorf("the administrator reads %v", administrator)
		}
		permissions := list(t, want(t, member.get(t, "/api/v1/permissions"), http.StatusOK, "list the permissions"), "permissions")
		if len(permissions) != 7 || permissions[6].(map[string]any)["permission"] != "administerInstallation" || permissions[6].(map[string]any)["scope"] != "installation" {
			t.Errorf("the permissions read %v", permissions)
		}
		refuse(t, "nobody reads the roles", nobody.get(t, "/api/v1/roles"), http.StatusUnauthorized, "unauthorized")
		refuse(t, "nobody reads the permissions", nobody.get(t, "/api/v1/permissions"), http.StatusUnauthorized, "unauthorized")
	})

	t.Run("an administrator adds, changes and deletes a role of the installation's own", func(t *testing.T) {
		made := obj(t, want(t, admin.post(t, "/api/v1/roles", map[string]any{"key": roleKey, "name": "Auditor " + roleKey, "permissions": []string{"view"}}),
			http.StatusCreated, "add a role"), "role")
		if made["role"] != roleKey || made["inUse"] != float64(0) {
			t.Fatalf("the role reads %v", made)
		}
		refuse(t, "a member adds a role", member.post(t, "/api/v1/roles", map[string]any{"name": "Mine"}), http.StatusForbidden, "forbidden")
		fieldError(t, admin.post(t, "/api/v1/roles", map[string]any{"name": " "}), "name")
		fieldError(t, admin.post(t, "/api/v1/roles", map[string]any{"name": "Flyer", "permissions": []string{"fly"}}), "permissions")
		fieldError(t, admin.post(t, "/api/v1/roles", map[string]any{"name": "Auditor " + roleKey}), "name")

		changed := obj(t, want(t, admin.patch(t, "/api/v1/roles/"+roleKey, map[string]any{"permissions": []string{"view", "comment"}}), http.StatusOK, "tick comment"), "role")
		if !slices.Equal(changed["permissions"].([]any), []any{"view", "comment"}) {
			t.Errorf("the role holds %v", changed["permissions"])
		}
		refuse(t, "a member ticks a box", member.patch(t, "/api/v1/roles/"+roleKey, map[string]any{"permissions": []string{"view", "edit"}}), http.StatusForbidden, "forbidden")
		fieldError(t, admin.patch(t, "/api/v1/roles/"+roleKey, map[string]any{"name": ""}), "name")
		fieldError(t, admin.patch(t, "/api/v1/roles/administrator", map[string]any{"permissions": []string{"view"}}), "permissions")
		refuse(t, "a role nobody defined", admin.patch(t, "/api/v1/roles/nothing_here", map[string]any{"name": "Gone"}), http.StatusNotFound, "not_found")

		refuse(t, "a member deletes a role", member.delete(t, "/api/v1/roles/"+roleKey), http.StatusForbidden, "forbidden")
		refuse(t, "deleting a built-in role", admin.delete(t, "/api/v1/roles/viewer"), http.StatusConflict, "conflict")
		want(t, admin.delete(t, "/api/v1/roles/"+roleKey), http.StatusNoContent, "delete the role")
		refuse(t, "deleting it twice", admin.delete(t, "/api/v1/roles/"+roleKey), http.StatusNotFound, "not_found")
	})

	// The group outlives the subtest that makes it: the ones after grant, empty and delete it.
	whole := t
	var groupID string
	t.Run("an administrator makes a group, fills it by hand and renames it", func(t *testing.T) {
		name := "Auditors " + h.suffix()
		group := obj(t, want(t, admin.post(t, "/api/v1/groups", map[string]any{"name": name, "description": "Who audits"}), http.StatusCreated, "make a group"), "group")
		groupID = group["id"].(string)
		whole.Cleanup(func() { h.cleanupExec(whole, h.super, `DELETE FROM groups WHERE id = $1`, groupID) })
		if group["source"] != "local" || group["memberCount"] != float64(0) {
			t.Errorf("the group reads %v", group)
		}
		refuse(t, "a member makes a group", member.post(t, "/api/v1/groups", map[string]any{"name": "Mine " + h.suffix()}), http.StatusForbidden, "forbidden")
		fieldError(t, admin.post(t, "/api/v1/groups", map[string]any{"name": ""}), "name")
		fieldError(t, admin.post(t, "/api/v1/groups", map[string]any{"name": name}), "name")

		filled := obj(t, want(t, admin.post(t, "/api/v1/groups/"+groupID+"/members", map[string]any{"userId": memberID}), http.StatusOK, "put a member in"), "group")
		if filled["memberCount"] != float64(1) || len(filled["members"].([]any)) != 1 {
			t.Errorf("the group holds %v", filled)
		}
		refuse(t, "a member fills a group", member.post(t, "/api/v1/groups/"+groupID+"/members", map[string]any{"userId": otherID}), http.StatusForbidden, "forbidden")
		fieldError(t, admin.post(t, "/api/v1/groups/"+groupID+"/members", map[string]any{"userId": uuid.New()}), "userId")

		read := obj(t, want(t, admin.get(t, "/api/v1/groups/"+groupID), http.StatusOK, "read the group"), "group")
		if read["members"].([]any)[0].(map[string]any)["name"] != "Mia Member" {
			t.Errorf("the group's members read %v", read["members"])
		}
		refuse(t, "a member reads a group's people", member.get(t, "/api/v1/groups/"+groupID), http.StatusForbidden, "forbidden")
		refuse(t, "a group that is none", admin.get(t, "/api/v1/groups/"+uuid.NewString()), http.StatusNotFound, "not_found")

		renamed := obj(t, want(t, admin.patch(t, "/api/v1/groups/"+groupID, map[string]any{"name": name + " renamed"}), http.StatusOK, "rename the group"), "group")
		if renamed["name"] != name+" renamed" {
			t.Errorf("the group reads %v", renamed)
		}
		refuse(t, "a member renames a group", member.patch(t, "/api/v1/groups/"+groupID, map[string]any{"name": "Taken"}), http.StatusForbidden, "forbidden")
		fieldError(t, admin.patch(t, "/api/v1/groups/"+groupID, map[string]any{"name": ""}), "name")
	})

	t.Run("Everyone holds every member and is changed by nobody", func(t *testing.T) {
		everyone := h.everyoneGroup(t).String()
		group := obj(t, want(t, admin.get(t, "/api/v1/groups/"+everyone), http.StatusOK, "read Everyone"), "group")
		var ids []string
		for _, m := range group["members"].([]any) {
			ids = append(ids, m.(map[string]any)["id"].(string))
		}
		if group["builtin"] != "everyone" || !slices.Contains(ids, memberID.String()) || slices.Contains(ids, guestID.String()) {
			t.Errorf("Everyone reads %v, want every member and no guest", group["name"])
		}
		refuse(t, "renaming Everyone", admin.patch(t, "/api/v1/groups/"+everyone, map[string]any{"name": "All"}), http.StatusConflict, "conflict")
		refuse(t, "deleting Everyone", admin.delete(t, "/api/v1/groups/"+everyone), http.StatusConflict, "conflict")
		refuse(t, "filling Everyone by hand", admin.post(t, "/api/v1/groups/"+everyone+"/members", map[string]any{"userId": guestID}), http.StatusConflict, "conflict")
		refuse(t, "taking somebody out of Everyone", admin.delete(t, "/api/v1/groups/"+everyone+"/members/"+memberID.String()), http.StatusConflict, "conflict")
	})

	t.Run("a grant across the installation is an administrator's to give and take", func(t *testing.T) {
		given := obj(t, want(t, admin.post(t, "/api/v1/role-assignments", map[string]any{"role": "viewer", "groupId": groupID}), http.StatusCreated, "grant the group viewer"), "assignment")
		id := given["id"].(string)
		if given["projectId"] != nil || given["groupName"] == "" {
			t.Errorf("the grant reads %v", given)
		}
		page := want(t, admin.get(t, "/api/v1/role-assignments?scope=installation&role=viewer&limit=200"), http.StatusOK, "list the grants across the installation")
		if found := withID(list(t, page, "assignments"), uuid.MustParse(id)); found == nil || page.Body["total"].(float64) < 1 {
			t.Errorf("the grant is not listed: %s", page.Raw)
		}
		groups := list(t, want(t, admin.get(t, "/api/v1/groups?q=Auditors&limit=50"), http.StatusOK, "the groups"), "groups")
		if found := withID(groups, uuid.MustParse(groupID)); found == nil || found["roleCount"] != float64(1) {
			t.Errorf("the group counts %v grants", found)
		}
		refuse(t, "a member lists the grants across the installation", member.get(t, "/api/v1/role-assignments"), http.StatusForbidden, "forbidden")
		refuse(t, "a scope that is none", admin.get(t, "/api/v1/role-assignments?scope=galaxy"), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a member grants across the installation", member.post(t, "/api/v1/role-assignments", map[string]any{"role": "viewer", "userId": memberID}), http.StatusForbidden, "forbidden")
		fieldError(t, admin.post(t, "/api/v1/role-assignments", map[string]any{"role": "viewer", "userId": guestID}), "userId")
		fieldError(t, admin.post(t, "/api/v1/role-assignments", map[string]any{"userId": memberID}), "role")
		refuse(t, "a member takes a grant across the installation, which they cannot see", member.delete(t, "/api/v1/role-assignments/"+id), http.StatusForbidden, "forbidden")
		refuse(t, "a malformed grant id", admin.delete(t, "/api/v1/role-assignments/nothing"), http.StatusBadRequest, "bad_request")
		want(t, admin.delete(t, "/api/v1/role-assignments/"+id), http.StatusNoContent, "take the grant")
		refuse(t, "taking it twice", admin.delete(t, "/api/v1/role-assignments/"+id), http.StatusNotFound, "not_found")
	})

	t.Run("an administrator empties a group and deletes it with its grants", func(t *testing.T) {
		refuse(t, "a member takes somebody out", member.delete(t, "/api/v1/groups/"+groupID+"/members/"+memberID.String()), http.StatusForbidden, "forbidden")
		emptied := obj(t, want(t, admin.delete(t, "/api/v1/groups/"+groupID+"/members/"+memberID.String()), http.StatusOK, "take the member out"), "group")
		if emptied["memberCount"] != float64(0) {
			t.Errorf("the group still holds %v", emptied)
		}
		refuse(t, "taking out somebody not in it", admin.delete(t, "/api/v1/groups/"+groupID+"/members/"+memberID.String()), http.StatusUnprocessableEntity, "validation_failed")
		want(t, admin.post(t, "/api/v1/role-assignments", map[string]any{"role": "viewer", "groupId": groupID}), http.StatusCreated, "grant the group again")
		refuse(t, "a member deletes a group", member.delete(t, "/api/v1/groups/"+groupID), http.StatusForbidden, "forbidden")
		want(t, admin.delete(t, "/api/v1/groups/"+groupID), http.StatusNoContent, "delete the group")
		if n := h.countRows(t, `SELECT count(*) FROM role_assignment WHERE group_id = $1`, groupID); n != 0 {
			t.Errorf("the deleted group's %d grants stayed", n)
		}
	})

	t.Run("a provider's group is filled by the provider alone", func(t *testing.T) {
		ref := "auditors-" + h.suffix()
		group := obj(t, want(t, admin.post(t, "/api/v1/groups", map[string]any{"name": ref, "externalRef": ref}), http.StatusCreated, "make a provider's group"), "group")
		id := group["id"].(string)
		t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM groups WHERE id = $1`, id) })
		if group["source"] != "oidc" || group["fromProvider"] != true || group["externalRef"] != ref {
			t.Errorf("the provider's group reads %v", group)
		}
		refuse(t, "filling a provider's group by hand", admin.post(t, "/api/v1/groups/"+id+"/members", map[string]any{"userId": memberID}), http.StatusConflict, "conflict")
		fieldError(t, admin.post(t, "/api/v1/groups", map[string]any{"name": "Other " + ref, "externalRef": ref}), "externalRef")
	})

	t.Run("each change is on the record", func(t *testing.T) {
		for action, n := range map[string]int{
			"role.created": 1, "role.updated": 1, "role.deleted": 1, "group.created": 2, "group.updated": 1, "group.deleted": 1,
			"group.member_added": 1, "group.member_removed": 1, "grant.added": 2, "grant.removed": 1,
		} {
			if got := h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = $1 AND actor_user_id = $2`, action, home.user); got != n {
				t.Errorf("%s was recorded %d times, want %d", action, got, n)
			}
		}
	})
}
