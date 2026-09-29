package toolcatalog

import (
	"strings"
	"testing"
)

func TestManagedHermesImageCanonicalizesToolOrder(t *testing.T) {
	first := ManagedHermesImage("project", []string{"pdf", "ocr"}, "v1", "digest-a", "snapshot")
	second := ManagedHermesImage("project", []string{"ocr", "pdf"}, "v1", "digest-a", "snapshot")
	if first != second {
		t.Fatalf("tool order changed managed image: %q != %q", first, second)
	}
}

func TestManagedHermesImageSeparatesProjectsAndBaseImages(t *testing.T) {
	base := ManagedHermesImage("project", []string{"pdf"}, "v1", "digest-a", "snapshot")
	for _, different := range []string{
		ManagedHermesImage("other", []string{"pdf"}, "v1", "digest-a", "snapshot"),
		ManagedHermesImage("project", []string{"ocr"}, "v1", "digest-a", "snapshot"),
		ManagedHermesImage("project", []string{"pdf"}, "v2", "digest-a", "snapshot"),
		ManagedHermesImage("project", []string{"pdf"}, "v1", "digest-b", "snapshot"),
		ManagedHermesImage("project", []string{"pdf"}, "v1", "digest-a", "other-snapshot"),
	} {
		if base == different {
			t.Fatalf("managed image identity collision: %q", base)
		}
	}
}

func TestManagedHermesImageBoundsLongProjectNames(t *testing.T) {
	image := ManagedHermesImage(strings.Repeat("p", 200), nil, "v1", "digest", "snapshot")
	if len(image) > 128 {
		t.Fatalf("managed image exceeds Docker tag length: %d", len(image))
	}
}
