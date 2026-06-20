package ui

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func rotateImageFile(path string, degrees int) (string, error) {
	deg := ((degrees % 360) + 360) % 360
	if deg != 0 && deg != 90 && deg != 180 && deg != 270 {
		return "", errors.New("rotation must be a multiple of 90 degrees")
	}
	src, err := decodeImageFile(path)
	if err != nil {
		return "", err
	}
	return saveTransformedImage(rotateImage(src, deg), path, fmt.Sprintf("rot%d", deg))
}

func flipImageFile(path string, horizontal bool) (string, error) {
	src, err := decodeImageFile(path)
	if err != nil {
		return "", err
	}
	suffix := "flipv"
	if horizontal {
		suffix = "fliph"
	}
	return saveTransformedImage(flipImage(src, horizontal), path, suffix)
}

func rotateImage(src image.Image, deg int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if deg == 0 {
		return src
	}
	var dst *image.RGBA
	if deg == 180 {
		dst = image.NewRGBA(image.Rect(0, 0, w, h))
	} else {
		dst = image.NewRGBA(image.Rect(0, 0, h, w))
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := src.At(b.Min.X+x, b.Min.Y+y)
			switch deg {
			case 90:
				dst.Set(h-1-y, x, c)
			case 180:
				dst.Set(w-1-x, h-1-y, c)
			case 270:
				dst.Set(y, w-1-x, c)
			}
		}
	}
	return dst
}

func flipImage(src image.Image, horizontal bool) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.Transparent, image.Point{}, draw.Src)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := src.At(b.Min.X+x, b.Min.Y+y)
			if horizontal {
				dst.Set(w-1-x, y, c)
			} else {
				dst.Set(x, h-1-y, c)
			}
		}
	}
	return dst
}

func saveTransformedImage(img image.Image, originalPath string, suffix string) (string, error) {
	outputPath, format, err := prepareTransformOutput(originalPath, suffix)
	if err != nil {
		return "", err
	}
	file, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	defer file.Close()

	switch format {
	case "jpeg":
		if err := jpeg.Encode(file, img, &jpeg.Options{Quality: 92}); err != nil {
			return "", err
		}
	default:
		if err := png.Encode(file, img); err != nil {
			return "", err
		}
	}
	return outputPath, nil
}

func prepareTransformOutput(originalPath string, suffix string) (string, string, error) {
	baseDir := filepath.Join(filepath.Dir(strings.TrimSpace(originalPath)), "imports")
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return "", "", err
	}
	base := filepath.Base(originalPath)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	ext, format := transformEncodingForPath(originalPath)
	name := fmt.Sprintf("%s-%s-%s%s", time.Now().Format("20060102-150405"), sanitizeFileStem(stem), suffix, ext)
	outputPath, err := filepath.Abs(filepath.Join(baseDir, name))
	if err != nil {
		return "", "", err
	}
	return outputPath, format, nil
}

func transformEncodingForPath(originalPath string) (string, string) {
	switch strings.ToLower(filepath.Ext(originalPath)) {
	case ".jpg", ".jpeg":
		return ".jpg", "jpeg"
	default:
		return ".png", "png"
	}
}

func sanitizeFileStem(input string) string {
	input = strings.ToLower(strings.TrimSpace(input))
	input = strings.ReplaceAll(input, " ", "-")
	out := make([]rune, 0, len(input))
	for _, r := range input {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return "image"
	}
	return string(out)
}
