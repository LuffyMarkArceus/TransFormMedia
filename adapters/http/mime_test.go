package http

import "testing"

func TestNormalizeContentType(t *testing.T) {
	if got := normalizeContentType("image/jpg"); got != "image/jpeg" {
		t.Fatalf("expected image/jpeg, got %q", got)
	}
}

func TestIsSupportedMediaType_RejectsSpoofedHeaderTypes(t *testing.T) {
	if isSupportedMediaType("application/javascript") {
		t.Fatal("expected unsupported type")
	}
	if !isSupportedMediaType("image/png") {
		t.Fatal("expected image/png to be supported")
	}
}
