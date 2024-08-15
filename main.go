package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
)

type Header struct {
	Magic      string
	Width      uint32
	Height     uint32
	Channels   uint8
	Colorspace uint8
}

const MAGIC_BYTES string = "qoif"

func ReadHeader(file []byte) (*Header, error) {
	if len(file) < 14 {
		return nil, fmt.Errorf("QOI Header is 14 bytes long, got %d bytes", len(file))
	}

	magic := file[0:4]
	if string(magic) != MAGIC_BYTES {
		return nil, fmt.Errorf("file does not start with QOI magic bytes, found %s", magic)
	}

	width := binary.BigEndian.Uint32(file[4:8])
	height := binary.BigEndian.Uint32(file[8:12])
	channels := uint8(file[12:13][0])
	colorspace := uint8(file[13:14][0])

	return &Header{Magic: string(magic), Width: width, Height: height, Channels: channels, Colorspace: colorspace}, nil
}

type Pixel struct {
	R uint8
	G uint8
	B uint8
	A uint8
}

func (p Pixel) Hash() uint8 {
	return (p.R*3 + p.G*5 + p.B*7 + p.A*11) % 64
}

func (p Pixel) Equals(other Pixel) bool {
	return (p.R == other.R) && (p.G == other.G) && (p.B == other.B) && (p.A == other.A)
}

type ChunkType int

const (
	UNKNOWN ChunkType = iota
	QOI_OP_RGB
	QOI_OP_RGBA
	QOI_OP_INDEX
	QOI_OP_DIFF
	QOI_OP_LUMA
	QOI_OP_RUN
)

type State struct {
	Raw           []Pixel
	historyBuffer [64]Pixel
	previousPixel Pixel
	previousType  ChunkType
	Header
}

func NewState() State {
	state := State{}

	state.previousPixel = Pixel{R: 0, G: 0, B: 0, A: 255}
	state.previousType = UNKNOWN
	return state
}

func Decode(buffer []byte) (*State, error) {

	header, err := ReadHeader(buffer)
	if err != nil {
		return nil, err
	}

	s := NewState()
	s.Header = *header
	var expectedPixelsCount int = int(s.Width * s.Height)
	s.Raw = make([]Pixel, expectedPixelsCount)

	idx := 14 // header length
	pixelsRead := 0

PixelLoop:
	for idx < len(buffer) && pixelsRead < expectedPixelsCount {
		tag := buffer[idx]
		switch {
		case tag == 255:
			// fmt.Printf("idx %d has 'QOI_OP_RGBA' chunk\n", idx)
			pixel := Pixel{R: buffer[idx+1], G: buffer[idx+2], B: buffer[idx+3], A: buffer[idx+4]}
			s.historyBuffer[pixel.Hash()] = pixel
			s.Raw[pixelsRead] = pixel
			s.previousPixel = pixel
			idx += 5
			pixelsRead += 1

		case tag == 254:
			// fmt.Printf("idx %d has 'QOI_OP_RGB' chunk\n", idx)
			pixel := Pixel{R: buffer[idx+1], G: buffer[idx+2], B: buffer[idx+3], A: s.previousPixel.A}
			s.historyBuffer[pixel.Hash()] = pixel
			s.Raw[pixelsRead] = pixel
			s.previousPixel = pixel
			idx += 4
			pixelsRead += 1

		case expectedPixelsCount == pixelsRead:
			// fmt.Printf("idx %d end marker -  tag %b \n", idx, buffer[idx:])
			break PixelLoop

		case (tag >> 6) == 0:
			pixel := s.historyBuffer[tag]
			// fmt.Printf("idx %d has 'QOI_OP_INDEX' chunk -  tag %08b - historyBufferIdx %d Pixel %v \n", idx, tag, tag, pixel)
			pixel = Pixel{R: pixel.R, G: pixel.G, B: pixel.B, A: pixel.A}
			s.Raw[pixelsRead] = pixel
			s.previousPixel = pixel
			idx += 1
			pixelsRead += 1

		case (tag >> 6) == 1:
			var bias byte = 2
			// fmt.Printf("idx %d has 'QOI_OP_DIFF' chunk -  tag %08b - %d ", idx, tag, tag>>6)
			rMask := byte(0b00110000)
			gMask := byte(0b00001100)
			bMask := byte(0b00000011)

			r := s.previousPixel.R + ((tag & rMask) >> 4) - bias
			g := s.previousPixel.G + ((tag & gMask) >> 2) - bias
			b := s.previousPixel.B + ((tag & bMask) >> 0) - bias
			a := s.previousPixel.A

			pixel := Pixel{R: r, G: g, B: b, A: a}

			// fmt.Printf("Pixel %v  Hash %v \n", pixel, pixel.Hash())

			s.historyBuffer[pixel.Hash()] = pixel
			s.Raw[pixelsRead] = pixel
			s.previousPixel = pixel
			idx += 1
			pixelsRead += 1

		case (tag >> 6) == 2:
			// fmt.Printf("idx %d has 'QOI_OP_LUMA' chunk -  tag %08b - %d ", idx, tag, tag>>6)

			pixel := Pixel{A: s.previousPixel.A}

			dgBias := byte(32)
			dgMask := byte(0b00111111)

			drDgBias := byte(8)
			drDgMask := byte(0b11110000)

			dbDgBias := byte(8)
			dbDgMask := byte(0b00001111)

			rbByte := buffer[idx+1]

			pixel.G = (tag & dgMask) - dgBias + s.previousPixel.G
			pixel.R = ((rbByte & drDgMask) >> 4) - drDgBias + s.previousPixel.R + pixel.G - s.previousPixel.G
			pixel.B = (rbByte & dbDgMask) - dbDgBias + s.previousPixel.B + pixel.G - s.previousPixel.G

			s.historyBuffer[pixel.Hash()] = pixel
			s.Raw[pixelsRead] = pixel
			s.previousPixel = pixel

			// fmt.Printf("%08b Pixel %v \n", buffer[idx:idx+2], pixel)

			idx += 2
			pixelsRead += 1
		case (tag >> 6) == 3:
			runLength := int((tag<<2)>>2) + 1
			// fmt.Printf("idx %d has 'QOI_OP_RUN' chunk -  tag %08b - RUN - %d \n", idx, tag, runLength)
			if pixelsRead == 0 {
				s.historyBuffer[s.previousPixel.Hash()] = s.previousPixel // https://github.com/phoboslab/qoi/issues/258
			}
			for rIdx := pixelsRead; rIdx < pixelsRead+runLength; rIdx++ {
				s.Raw[rIdx] = Pixel{R: s.previousPixel.R, G: s.previousPixel.G, B: s.previousPixel.B, A: s.previousPixel.A}
				s.previousPixel = s.Raw[rIdx]
			}
			idx += 1
			pixelsRead += runLength
		}
	}

	if pixelsRead != expectedPixelsCount {
		return nil, fmt.Errorf("expected %d Pixels, found %d ", expectedPixelsCount, pixelsRead)
	}

	return &s, nil
}

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
		currPixel := Pixel{
			R: rgba[idx],
			G: rgba[idx+1],
			B: rgba[idx+2],
			A: rgba[idx+3],
		}

		if s.historyBuffer[currPixel.Hash()].Equals(currPixel) {
			// check if previous chunk was also a QOI_OP_INDEX hashed to same index
			if s.previousType == QOI_OP_INDEX && s.previousPixel.Hash() == currPixel.Hash() {
				// spec disallows 2 consecutive QOI_OP_INDEX hashed to same index
				var count uint8 = 0 // bias of 1
				rIdx := idx + 4
				for pixelsWritten < expectedPixelsCount && rIdx < len(rgba) && count < 61 {
					// TODO handle images with no alpha channel?
					// fmt.Printf("prev idx -> checking runlength rIdx: %d idx: %d count: %d\n", rIdx, idx, count)
					runPixel := Pixel{
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
				s.previousType = QOI_OP_RUN
				s.previousPixel = currPixel
				s.historyBuffer[currPixel.Hash()] = currPixel
				buffer = append(buffer, count|0b11000000)
				pixelsWritten += (uint32(count) + 1)
				// fmt.Printf("Writing a QOI_OP_RUN Chunk %08b\n", buffer[len(buffer)-1])
				continue
			} else {
				// QOI_OP_INDEX
				idx += 4
				s.previousType = QOI_OP_INDEX
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
				runPixel := Pixel{
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
			s.previousType = QOI_OP_RUN
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
					s.previousType = QOI_OP_DIFF
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
					s.previousType = QOI_OP_LUMA
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
				s.previousType = QOI_OP_RGB
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
				s.previousType = QOI_OP_RGBA
				s.previousPixel = currPixel
				s.historyBuffer[currPixel.Hash()] = currPixel
				buffer = append(buffer, 0b11111111)
				buffer = append(buffer, currPixel.R)
				buffer = append(buffer, currPixel.G)
				buffer = append(buffer, currPixel.B)
				buffer = append(buffer, currPixel.A)
				pixelsWritten += 1
				fmt.Printf("Writing a QOI_OP_RGBA Chunk %08b %08b %08b %08b %08b\n", buffer[len(buffer)-5], buffer[len(buffer)-4], buffer[len(buffer)-3], buffer[len(buffer)-2], buffer[len(buffer)-1])
				continue
			}
		}
	}

	return buffer, nil
}

func testDecode() *State {
	// file, err := os.ReadFile("./testimages/dice.qoi")
	// file, err := os.ReadFile("./testimages/edgecase.qoi")
	file, err := os.ReadFile("./testimages/testcard_rgba.qoi")
	// file, err := os.ReadFile("./testimages/kodim10.qoi")
	// file, err := os.ReadFile("./testimages/kodim23.qoi")
	// file, err := os.ReadFile("./testimages/wikipedia_008.qoi")

	if err != nil {
		log.Fatalf("failed to read file %v", err)
	}

	qoiState, err := Decode(file)
	if err != nil {
		log.Fatalf("Failed to decode QOI from buffer: %v", err)
	}
	fmt.Printf("%v\n", qoiState.Header)

	var outputBuffer []byte = make([]byte, len(qoiState.Raw)*4)

	for idx, buf := range qoiState.Raw {
		offset := idx * 4
		outputBuffer[offset] = buf.R
		outputBuffer[offset+1] = buf.G
		outputBuffer[offset+2] = buf.B
		outputBuffer[offset+3] = buf.A
	}

	err = os.WriteFile("./output/output.bin", outputBuffer, 0644)
	if err != nil {
		log.Fatalf("failed to write output file: %v", err)
	}
	return qoiState
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

func main() {
	state := testDecode()
	testEncode(state)
}
