package sourcepaths

import (
	"reflect"
	"testing"
)

func TestNormalize_CombinesLegacyAndList(t *testing.T) {
	got := Normalize(" docs/ ", []string{"examples/", "", "docs/", " api/ "})
	want := []string{"docs/", "examples/", "api/"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Normalize() = %#v, want %#v", got, want)
	}
}

func TestNormalize_EmptyReturnsEmptySlice(t *testing.T) {
	got := Normalize("", nil)
	if got == nil {
		t.Fatal("Normalize returned nil, want empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("Normalize length = %d, want 0", len(got))
	}
}

func TestFirst_ReturnsFirstOrEmpty(t *testing.T) {
	if got := First([]string{"docs/", "api/"}); got != "docs/" {
		t.Fatalf("First() = %q, want docs/", got)
	}
	if got := First(nil); got != "" {
		t.Fatalf("First(nil) = %q, want empty", got)
	}
}
