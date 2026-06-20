package ui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestRotateImageFileSwapsDimensions(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source.png")
	writeTestPNG(t, src, 4, 2)

	out, err := rotateImageFile(src, 90)
	if err != nil {
		t.Fatalf("rotateImageFile: %v", err)
	}
	img, err := decodeImageFile(out)
	if err != nil {
		t.Fatalf("decodeImageFile: %v", err)
	}
	if got, want := img.Bounds().Dx(), 2; got != want {
		t.Fatalf("rotated width=%d want %d", got, want)
	}
	if got, want := img.Bounds().Dy(), 4; got != want {
		t.Fatalf("rotated height=%d want %d", got, want)
	}
}

func TestFlipImageFileKeepsDimensions(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source.png")
	writeTestPNG(t, src, 3, 5)

	out, err := flipImageFile(src, true)
	if err != nil {
		t.Fatalf("flipImageFile: %v", err)
	}
	img, err := decodeImageFile(out)
	if err != nil {
		t.Fatalf("decodeImageFile: %v", err)
	}
	if got, want := img.Bounds().Dx(), 3; got != want {
		t.Fatalf("flipped width=%d want %d", got, want)
	}
	if got, want := img.Bounds().Dy(), 5; got != want {
		t.Fatalf("flipped height=%d want %d", got, want)
	}
}

func writeTestPNG(t *testing.T, path string, width int, height int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.NRGBA{R: uint8(x * 10), G: uint8(y * 10), B: 120, A: 255})
		}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("os.Create: %v", err)
	}
	defer file.Close()
	if err := png.Encode(file, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
}
