package qoi

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	"io"
	"log"
	"os"
)


func Encode(rgba []byte, height uint32, width uint32, channels uint8, colorspace uint8) ([]byte, error) {
	expectedPixelsCount := height * width

	// might need channel handling
	if len(rgba) != int(expectedPixelsCount)*4 {
		return nil, fmt.Errorf("insufficient rgba data for the expected height and width, h: %d w: %d r: %d required: %d", height, width, len(rgba), int(expectedPixelsCount)*int(channels))
	}

	buffer := []byte(MAGIC_BYTES)
	buffer = binary.BigEndian.AppendUint32(buffer, width)
	buffer = binary.BigEndian.AppendUint32(buffer, height)
	buffer = append(buffer, channels, colorspace)

	if len(buffer) != 14 {
		panic(fmt.Sprintf("Header was encoded incorrectly, expect 14 bytes, found %d bytes. encoded header - %08b", len(buffer), buffer))
	}

	s := NewState()

	idx := 0
	var pixelsWritten uint32 = 0

	for pixelsWritten < expectedPixelsCount && idx < len(rgba) {
		currPixel := pixel{
			R: rgba[idx],
			G: rgba[idx+1],
			B: rgba[idx+2],
			A: rgba[idx+3],
		}

		if s.historyBuffer[currPixel.Hash()].Equals(currPixel) {
			// check if previous chunk was also a QOI_OP_INDEX hashed to same index
			if s.previousType == qoi_op_index && s.previousPixel.Hash() == currPixel.Hash() {
				// spec disallows 2 consecutive QOI_OP_INDEX hashed to same index
				var count uint8 = 0 // bias of 1
				rIdx := idx + 4
				for pixelsWritten < expectedPixelsCount && rIdx < len(rgba) && count < 61 {
					// TODO handle images with no alpha channel?
					// fmt.Printf("prev idx -> checking runlength rIdx: %d idx: %d count: %d\n", rIdx, idx, count)
					runPixel := pixel{
						R: rgba[rIdx],
						G: rgba[rIdx+1],
						B: rgba[rIdx+2],
						A: rgba[rIdx+3],
					}
					if currPixel.Equals(runPixel) {
						count += 1
						rIdx += 4
					} else {
						break
					}
				}
				idx = rIdx
				s.previousType = qoi_op_run
				s.previousPixel = currPixel
				s.historyBuffer[currPixel.Hash()] = currPixel
				buffer = append(buffer, count|0b11000000)
				pixelsWritten += (uint32(count) + 1)
				// fmt.Printf("Writing a QOI_OP_RUN Chunk %08b\n", buffer[len(buffer)-1])
				continue
			} else {
				// QOI_OP_INDEX
				idx += 4
				s.previousType = qoi_op_index
				s.previousPixel = currPixel
				s.historyBuffer[currPixel.Hash()] = currPixel
				buffer = append(buffer, currPixel.Hash())
				pixelsWritten += 1
				// fmt.Printf("Writing a QOI_OP_INDEX Chunk %08b\n", buffer[len(buffer)-1])
				continue
			}
		}

		if currPixel.Equals(s.previousPixel) {
			var count uint8 = 0 // bias of 1
			rIdx := idx + 4
			for pixelsWritten < expectedPixelsCount && rIdx < len(rgba) && count < 61 {
				runPixel := pixel{
					R: rgba[rIdx],
					G: rgba[rIdx+1],
					B: rgba[rIdx+2],
					A: rgba[rIdx+3],
				}
				if currPixel.Equals(runPixel) {
					count += 1
					rIdx += 4
				} else {
					break
				}
			}
			idx = rIdx
			s.previousType = qoi_op_run
			s.previousPixel = currPixel
			s.historyBuffer[currPixel.Hash()] = currPixel
			buffer = append(buffer, count|0b11000000)
			pixelsWritten += (uint32(count) + 1)
			// fmt.Printf("Writing a QOI_OP_RUN Chunk %08b\n", buffer[len(buffer)-1])
			continue
		} else {
			// check if buffer can be stored as diff using either QOI_OP_DIFF or QOI_OP_LUMA
			if channels == 3 || currPixel.A == s.previousPixel.A {
				// check if QOI_OP_DIFF
				var bias uint8 = 2
				rDiff := currPixel.R - s.previousPixel.R + bias
				gDiff := currPixel.G - s.previousPixel.G + bias
				bDiff := currPixel.B - s.previousPixel.B + bias
				if rDiff < 4 && gDiff < 4 && bDiff < 4 {
					// valid QOI_OP_DIFF
					idx += 4
					s.previousType = qoi_op_diff
					s.previousPixel = currPixel
					s.historyBuffer[currPixel.Hash()] = currPixel
					buffer = append(buffer, 0b01000000|rDiff<<4|gDiff<<2|bDiff)
					pixelsWritten += 1
					// fmt.Printf("Writing a QOI_OP_DIFF Chunk %08b\n", buffer[len(buffer)-1])
					continue
				}

				// check if QOI_OP_LUMA
				var greenBias uint8 = 32
				var redBias uint8 = 8
				var blueBias uint8 = 8

				dg := currPixel.G - s.previousPixel.G + greenBias
				dr_dg := (currPixel.R - s.previousPixel.R) - (currPixel.G - s.previousPixel.G) + redBias
				db_dg := (currPixel.B - s.previousPixel.B) - (currPixel.G - s.previousPixel.G) + blueBias

				if dg <= 63 && dr_dg <= 15 && db_dg <= 15 {
					// valid QOI_OP_LUMA
					idx += 4
					s.previousType = qoi_op_luma
					s.previousPixel = currPixel
					s.historyBuffer[currPixel.Hash()] = currPixel
					buffer = append(buffer, 0b10000000|dg)
					buffer = append(buffer, dr_dg<<4|db_dg)
					pixelsWritten += 1
					// fmt.Printf("Writing a QOI_OP_LUMA Chunk %08b\n", buffer[len(buffer)-1])
					continue
				}

				// QOI_OP_RGB
				idx += 4
				s.previousType = qoi_op_rgb
				s.previousPixel = currPixel
				s.historyBuffer[currPixel.Hash()] = currPixel
				buffer = append(buffer, 0b11111110)
				buffer = append(buffer, currPixel.R)
				buffer = append(buffer, currPixel.G)
				buffer = append(buffer, currPixel.B)
				pixelsWritten += 1
				// fmt.Printf("Writing a QOI_OP_RGB Chunk %08b %08b %08b %08b\n", buffer[len(buffer)-4], buffer[len(buffer)-3], buffer[len(buffer)-2], buffer[len(buffer)-1])
				continue

			} else {
				// QOI_OP_RGBA
				idx += 4
				s.previousType = qoi_op_rgba
				s.previousPixel = currPixel
				s.historyBuffer[currPixel.Hash()] = currPixel
				buffer = append(buffer, 0b11111111)
				buffer = append(buffer, currPixel.R)
				buffer = append(buffer, currPixel.G)
				buffer = append(buffer, currPixel.B)
				buffer = append(buffer, currPixel.A)
				pixelsWritten += 1
				// fmt.Printf("Writing a QOI_OP_RGBA Chunk %08b %08b %08b %08b %08b\n", buffer[len(buffer)-5], buffer[len(buffer)-4], buffer[len(buffer)-3], buffer[len(buffer)-2], buffer[len(buffer)-1])
				continue
			}
		}
	}

	return buffer, nil
}

func imageToNRGBA(src image.Image)  *image.NRGBA {
	dst := image.NewNRGBA(src.Bounds())
	draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Src)
	return dst
}

func nrgbaImageToQOI(m *image.NRGBA) ([]byte, error) {
	return Encode(m.Pix, uint32(m.Bounds().Max.Y), uint32(m.Bounds().Max.X), 4, 0)
} 

func ImageEncode(w io.Writer, m image.Image) error {
	switch src := m.(type) {
	case *image.NRGBA:
		{
			data, err := nrgbaImageToQOI(src)
			if err != nil {
				return err
			}
			w.Write(data)
		}
	default:
		{
			nrgbaImage := imageToNRGBA(m)
			data, err := nrgbaImageToQOI(nrgbaImage)
			if err != nil {
				return err
			}
			w.Write(data)
		}
	}
	return nil
}

func testEncode(state *State) {
	file, err := os.ReadFile("./output/output.bin")
	// file, err := os.ReadFile("./testimages/edgecase.png")
	// file, err := os.ReadFile("./testimages/testcard_rgba.png")

	if err != nil {
		log.Fatalf("failed to read file %v", err)
	}

	qoiBuffer, err := Encode(file, state.Height, state.Width, state.Channels, state.Colorspace)
	if err != nil {
		log.Fatalf("Failed to encode QOI from raw: %v", err)
	}
	// fmt.Printf("%v\n", qoiBuffer)

	err = os.WriteFile("./output/output.qoi", qoiBuffer, 0644)
	if err != nil {
		log.Fatalf("failed to write output file: %v", err)
	}
}