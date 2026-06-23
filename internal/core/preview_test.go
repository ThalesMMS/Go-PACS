package core

import (
	"image"
	"testing"
)

func TestNormalizePreviewSizeParsesLarge(t *testing.T) {
	for _, input := range []string{"large", "Large", "LARGE", "  large  "} {
		if got := normalizePreviewSize(input); got != "large" {
			t.Fatalf("normalizePreviewSize(%q) = %q, want %q", input, got, "large")
		}
	}
}

func TestNormalizePreviewSizeDefaultsToThumb(t *testing.T) {
	for _, input := range []string{"", "thumb", "small", "medium", "unknown", "xl"} {
		if got := normalizePreviewSize(input); got != "thumb" {
			t.Fatalf("normalizePreviewSize(%q) = %q, want %q", input, got, "thumb")
		}
	}
}

func TestPreviewMaxDimensionReturnsThumbSize(t *testing.T) {
	if got := previewMaxDimension("thumb"); got != 256 {
		t.Fatalf("previewMaxDimension(thumb) = %d, want 256", got)
	}
}

func TestPreviewMaxDimensionReturnsLargeSize(t *testing.T) {
	if got := previewMaxDimension("large"); got != 1024 {
		t.Fatalf("previewMaxDimension(large) = %d, want 1024", got)
	}
}

func TestResizeNearestScalesDownWideImage(t *testing.T) {
	// Create a 640×100 image (wider than tall).
	src := image.NewRGBA(image.Rect(0, 0, 640, 100))
	dst := resizeNearest(src, 256)
	if dst == nil {
		t.Fatal("resizeNearest returned nil")
	}
	bounds := dst.Bounds()
	if bounds.Dx() != 256 {
		t.Fatalf("resized width = %d, want 256", bounds.Dx())
	}
	if bounds.Dy() == 0 {
		t.Fatal("resized height is 0")
	}
	// height should be proportional: 100 * 256 / 640 = 40
	if bounds.Dy() != 40 {
		t.Fatalf("resized height = %d, want 40", bounds.Dy())
	}
}

func TestResizeNearestScalesDownTallImage(t *testing.T) {
	// Create a 100×480 image (taller than wide).
	src := image.NewRGBA(image.Rect(0, 0, 100, 480))
	dst := resizeNearest(src, 256)
	bounds := dst.Bounds()
	if bounds.Dy() != 256 {
		t.Fatalf("resized height = %d, want 256", bounds.Dy())
	}
	if bounds.Dx() == 0 {
		t.Fatal("resized width is 0")
	}
	// width should be proportional: 100 * 256 / 480 = 53
	if bounds.Dx() != 53 {
		t.Fatalf("resized width = %d, want 53", bounds.Dx())
	}
}

func TestResizeNearestReturnsOriginalWhenAlreadySmall(t *testing.T) {
	// Image already fits within the maxDimension.
	src := image.NewRGBA(image.Rect(0, 0, 128, 64))
	dst := resizeNearest(src, 256)
	if dst != src {
		t.Fatal("resizeNearest should return the same image when it already fits")
	}
}

func TestResizeNearestReturnsOriginalWhenExactlyAtLimit(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 256, 256))
	dst := resizeNearest(src, 256)
	if dst != src {
		t.Fatal("resizeNearest should return the same image when exactly at maxDimension")
	}
}

func TestResizeNearestHandlesNilImage(t *testing.T) {
	if got := resizeNearest(nil, 256); got != nil {
		t.Fatal("resizeNearest(nil) should return nil")
	}
}

func TestResizeNearestHandlesZeroMaxDimension(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 512, 512))
	dst := resizeNearest(src, 0)
	if dst != src {
		t.Fatal("resizeNearest with maxDimension=0 should return original image unchanged")
	}
}

func TestResizeNearestHandlesNegativeMaxDimension(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 512, 512))
	dst := resizeNearest(src, -1)
	if dst != src {
		t.Fatal("resizeNearest with negative maxDimension should return original image unchanged")
	}
}
