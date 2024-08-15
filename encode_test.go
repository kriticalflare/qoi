package qoi_test

import (
	"bytes"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kriticalflare/qoi"
)

func TestImageEncoding(t *testing.T) {
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

		var buffer bytes.Buffer
		err = qoi.ImageEncode(&buffer, pngImg)
		if err != nil {
			t.Fatalf("failed to encode png to qoi: %v\n", err)
		}

		qoiFile, err := os.ReadFile(strings.Replace(pngFile, ".png", ".qoi", -1))
		if err != nil {
			t.Fatalf("failed to open qoi file '%v' due to %v\n", qoiFile, err)
		}

		if len(buffer.Bytes()) != len(qoiFile) {
			t.Fatalf("difference in byte length got=%v want=%v", len(buffer.Bytes()), len(qoiFile))
		}

		for idx, currByte := range buffer.Bytes() {
			if currByte != qoiFile[idx] {
				t.Logf("got=%08b\nwant=%08b\n", buffer.Bytes()[0: idx+1], qoiFile[0: idx+1])
				t.Fatalf("failed to encode qoi file from pngFile %v correctly got=%08b want=%08b at index %v", pngFile, currByte , qoiFile[idx], idx)
			}
		}		
	}
}
