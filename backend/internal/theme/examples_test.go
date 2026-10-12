package theme

import (
	"testing"

	"github.com/google/uuid"
)

// An example that the editor could not save would be a trap, so every one
// passes the same validation a typed theme does.
func TestEveryExampleIsAValidTheme(t *testing.T) {
	seen := map[string]bool{}
	for _, example := range Examples() {
		if example.Key == "" || example.Name == "" || seen[example.Key] {
			t.Errorf("example %q needs a key and a name of its own", example.Name)
		}
		seen[example.Key] = true
		spec := example.Spec
		if err := Validate(&spec, uuid.Nil, nil); err != nil {
			t.Errorf("%s does not validate: %v", example.Name, err)
		}
		if len(spec.Colors.Light) == 0 || len(spec.Colors.Dark) == 0 {
			t.Errorf("%s leaves one of the palettes empty", example.Name)
		}
	}
	if ExampleByKey("constellation").Spec.Effect != "constellation" {
		t.Error("Constellation does not ask the shell for its network")
	}
	bad := Spec{Effect: "fireworks"}
	if err := Validate(&bad, uuid.Nil, nil); err == nil {
		t.Error("an effect the shell cannot draw was accepted")
	}
	if ExampleByKey("deep-tech") == nil || ExampleByKey("nothing") != nil {
		t.Error("ExampleByKey does not find what is there, or finds what is not")
	}
}
