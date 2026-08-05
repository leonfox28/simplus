package openapi

import "testing"

func TestEmbeddedSpecValidates(t *testing.T) {
	spec, err := GetSwagger()
	if err != nil {
		t.Fatalf("GetSwagger() error = %v", err)
	}
	if err := spec.Validate(t.Context()); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}
