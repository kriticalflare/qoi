package qoi_test

import (
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kriticalflare/qoi"
)

func TestImageDecoding(t *testing.T) {
	image.RegisterFormat("qoi", qoi.MAGIC_BYTES, qoi.ImageDecode, qoi.DecodeConfig)
	pngFiles, err := filepath.Glob("./testimages/*.png")
	if err != nil {
		t.Fatalf("failed to read png files: %v\n", err)
	}
	for _, pngFile := range pngFiles {
		file, err := os.Open(pngFile)
		if err != nil {
			t.Fatalf("failed to open png file '%v' due to %v\n", pngFile, err)
		}
		pngImg, format, err := image.Decode(file)
		if err != nil {
			t.Fatalf("failed to decode png file '%v' due to '%v'\n", pngFile, err)
		}
		if format != "png" {
			t.Fatalf("invalid image format, got=%v, want=%v\n", format, "png")
		}

		qoiFile, err := os.Open(strings.Replace(pngFile, ".png", ".qoi", -1))
		if err != nil {
			t.Fatalf("failed to open qoi file '%v' due to %v\n", qoiFile, err)
		}
		qoiImg, format, err := image.Decode(qoiFile)

		if err != nil {
			t.Fatalf("failed to decode qoi file '%v' due to error: %v\n", qoiFile, err)
		}
		if format != "qoi" {
			t.Fatalf("invalid image format, got=%v, want=%v\n", format, "qoi")
		}

		if !pngImg.Bounds().Eq(qoiImg.Bounds()) {
			t.Fatalf("invalid bounds, png=%v qoi=%v", pngImg.Bounds(), qoiImg.Bounds())
		}

		if nrgbaPng, ok := pngImg.(*image.NRGBA); ok {
			nrgbaQoi := qoiImg.(*image.NRGBA)
			w := nrgbaPng.Rect.Dx()

			for x := nrgbaPng.Bounds().Min.X; x < nrgbaPng.Bounds().Max.X; x += 1 {
				for y := nrgbaPng.Bounds().Min.Y; y < nrgbaPng.Bounds().Max.Y; y += 1 {
					index := ((y * w) + x) * 4
					pngPixels := nrgbaPng.Pix[index : index+4]
					qoiPixels := nrgbaQoi.Pix[index : index+4]
					if !slices.Equal(pngPixels, qoiPixels) {
						t.Fatalf("png raw pixels != qoi raw pixels. png=%v qoi=%v", pngPixels, qoiPixels)
					}
				}
			}
		}
	}
}
