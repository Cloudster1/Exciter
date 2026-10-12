package openapi

// Stats shares its name with the Stats in package openapi_test, giving the
// clash tests two same-named types from two packages.
type Stats struct {
	Ready bool `json:"ready"`
}
