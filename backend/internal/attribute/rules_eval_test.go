package attribute

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestAFieldIsRequiredFromItsStatusOn(t *testing.T) {
	draft, review, approved := uuid.New(), uuid.New(), uuid.New()
	risk := Def{ID: uuid.New(), Name: "Risk", Kind: Text, RequiredFrom: &review}
	r := &Rules{Defs: []Def{risk}, Fixed: map[string]FieldRule{"owner": {Field: "owner", RequiredFrom: &approved}},
		order: map[uuid.UUID]int{draft: 0, review: 1, approved: 2}, status: map[uuid.UUID]string{draft: "Draft", review: "In review", approved: "Approved"}}
	if err := r.MissingIn(&draft, State{}); err != nil {
		t.Errorf("Draft asks for nothing, yet: %v", err)
	}
	var field *FieldError
	if err := r.MissingIn(&review, State{}); !errors.As(err, &field) || field.Field != "attributes."+risk.ID.String() ||
		field.Message != "Give Risk a value; requirements in In review need it." {
		t.Errorf("In review without Risk: %v", err)
	}
	full := State{Attributes: map[string]json.RawMessage{risk.ID.String(): json.RawMessage(`"low"`)}}
	if err := r.MissingIn(&review, full); err != nil {
		t.Errorf("In review with Risk: %v", err)
	}
	if err := r.MissingIn(&approved, full); !errors.As(err, &field) || field.Field != "ownerId" {
		t.Errorf("Approved without an owner: %v", err)
	}
	full.Owner = true
	if err := r.MissingIn(&approved, full); err != nil {
		t.Errorf("Approved with both: %v", err)
	}
}

func TestOnlyThePermittedChangeAField(t *testing.T) {
	draft, approved := uuid.New(), uuid.New()
	permits := []Permit{{Roles: []string{"editor"}, Statuses: []uuid.UUID{draft}}, {Roles: []string{"admin"}}}
	if ok, _ := Permitted(nil, &approved, []string{"viewer"}, "Risk", "Approved"); !ok {
		t.Error("a field without permits refused somebody")
	}
	if ok, _ := Permitted(permits, &draft, []string{"editor"}, "Risk", "Draft"); !ok {
		t.Error("an editor was refused in Draft")
	}
	if ok, why := Permitted(permits, &approved, []string{"editor", "reviewer"}, "Risk", "Approved"); ok || why != "Only admins change Risk in Approved." {
		t.Errorf("an editor in Approved: %v %q", ok, why)
	}
	if ok, _ := Permitted(permits, &approved, []string{"admin"}, "Risk", "Approved"); !ok {
		t.Error("an admin was refused in every status")
	}
}

func TestADependentChoiceNarrowsByTheOtherAndIsCheckedWhenEitherMoves(t *testing.T) {
	car, bike := Option{ID: uuid.New(), Name: "Car"}, Option{ID: uuid.New(), Name: "Bike"}
	disc, drum, rim := Option{ID: uuid.New(), Name: "Disc"}, Option{ID: uuid.New(), Name: "Drum"}, Option{ID: uuid.New(), Name: "Rim"}
	vehicle := Def{ID: uuid.New(), Name: "Vehicle", Kind: Select, Options: []Option{car, bike}}
	brake := Def{ID: uuid.New(), Name: "Brake", Kind: Select, Options: []Option{disc, drum, rim},
		DependsOn: &Dependency{AttributeID: vehicle.ID, Choices: map[string][]uuid.UUID{car.ID.String(): {disc.ID, drum.ID}}}}
	defs := []Def{vehicle, brake}
	values := func(v Option, b Option) map[string]json.RawMessage {
		return map[string]json.RawMessage{vehicle.ID.String(): encode(v.ID.String()), brake.ID.String(): encode(b.ID.String())}
	}
	if got := Allowed(defs, brake, values(car, disc)); len(got) != 2 {
		t.Errorf("a car allows %v", got)
	}
	if Allowed(defs, brake, values(bike, rim)) != nil {
		t.Error("a bike, which the choices do not name, narrows the brakes")
	}
	var field *FieldError
	err := CheckChoices(defs, nil, values(car, rim))
	if !errors.As(err, &field) || !strings.HasPrefix(field.Message, "Rim is not a choice of Brake while Vehicle is Car. Pick one of: Disc, Drum.") {
		t.Errorf("a rim brake on a car: %v", err)
	}
	if err := CheckChoices(defs, values(car, rim), values(car, rim)); err != nil {
		t.Errorf("values given before the rule, left as they were: %v", err)
	}
	if err := CheckChoices(defs, values(bike, rim), values(car, rim)); err == nil {
		t.Error("changing the vehicle under a rim brake went through")
	}
}
