package qoi

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"io"
	"log"
	"os"
)

type header struct {
	Magic      string
	Width      uint32
	Height     uint32
	Channels   uint8
	Colorspace uint8
}

const MAGIC_BYTES string = "qoif"

func readHeader(file []byte) (*header, error) {
	if len(file) < 14 {
		return nil, fmt.Errorf("QOI header is 14 bytes long, got %d bytes", len(file))
	}

	magic := file[0:4]
	if string(magic) != MAGIC_BYTES {
		return nil, fmt.Errorf("file does not start with QOI magic bytes, found %s", magic)
	}

	width := binary.BigEndian.Uint32(file[4:8])
	height := binary.BigEndian.Uint32(file[8:12])
	channels := uint8(file[12:13][0])
	colorspace := uint8(file[13:14][0])

	return &header{Magic: string(magic), Width: width, Height: height, Channels: channels, Colorspace: colorspace}, nil
}

type pixel struct {
	R uint8
	G uint8
	B uint8
	A uint8
}

func (p pixel) Hash() uint8 {
	return (p.R*3 + p.G*5 + p.B*7 + p.A*11) % 64
}

func (p pixel) Equals(other pixel) bool {
	return (p.R == other.R) && (p.G == other.G) && (p.B == other.B) && (p.A == other.A)
}

type chunkType int

const (
	UNKNOWN chunkType = iota
	qoi_op_rgb
	qoi_op_rgba
	qoi_op_index
	qoi_op_diff
	qoi_op_luma
	qoi_op_run
)

type State struct {
	Raw           []pixel
	historyBuffer [64]pixel
	previousPixel pixel
	previousType  chunkType
	header
}

func NewState() State {
	state := State{}

	state.previousPixel = pixel{R: 0, G: 0, B: 0, A: 255}
	state.previousType = UNKNOWN
	return state
}

func Decode(buffer []byte) (*State, error) {

	header, err := readHeader(buffer)
	if err != nil {
		return nil, err
	}

	s := NewState()
	s.header = *header
	var expectedPixelsCount int = int(s.Width * s.Height)
	s.Raw = make([]pixel, expectedPixelsCount)

	idx := 14 // header length
	pixelsRead := 0

PixelLoop:
	for idx < len(buffer) && pixelsRead < expectedPixelsCount {
		tag := buffer[idx]
		switch {
		case tag == 255:
			// fmt.Printf("idx %d has 'qoi_op_rgba' chunk\n", idx)
			pixel := pixel{R: buffer[idx+1], G: buffer[idx+2], B: buffer[idx+3], A: buffer[idx+4]}
			s.historyBuffer[pixel.Hash()] = pixel
			s.Raw[pixelsRead] = pixel
			s.previousPixel = pixel
			idx += 5
			pixelsRead += 1

		case tag == 254:
			// fmt.Printf("idx %d has 'qoi_op_rgb' chunk\n", idx)
			pixel := pixel{R: buffer[idx+1], G: buffer[idx+2], B: buffer[idx+3], A: s.previousPixel.A}
			s.historyBuffer[pixel.Hash()] = pixel
			s.Raw[pixelsRead] = pixel
			s.previousPixel = pixel
			idx += 4
			pixelsRead += 1

		case expectedPixelsCount == pixelsRead:
			// fmt.Printf("idx %d end marker -  tag %b \n", idx, buffer[idx:])
			break PixelLoop

		case (tag >> 6) == 0:
			pix := s.historyBuffer[tag]
			// fmt.Printf("idx %d has 'qoi_op_index' chunk -  tag %08b - historyBufferIdx %d Pixel %v \n", idx, tag, tag, pixel)
			pix = pixel{R: pix.R, G: pix.G, B: pix.B, A: pix.A}
			s.Raw[pixelsRead] = pix
			s.previousPixel = pix
			idx += 1
			pixelsRead += 1

		case (tag >> 6) == 1:
			var bias byte = 2
			// fmt.Printf("idx %d has 'qoi_op_diff' chunk -  tag %08b - %d ", idx, tag, tag>>6)
			rMask := byte(0b00110000)
			gMask := byte(0b00001100)
			bMask := byte(0b00000011)

			r := s.previousPixel.R + ((tag & rMask) >> 4) - bias
			g := s.previousPixel.G + ((tag & gMask) >> 2) - bias
			b := s.previousPixel.B + ((tag & bMask) >> 0) - bias
			a := s.previousPixel.A

			pixel := pixel{R: r, G: g, B: b, A: a}

			// fmt.Printf("Pixel %v  Hash %v \n", pixel, pixel.Hash())

			s.historyBuffer[pixel.Hash()] = pixel
			s.Raw[pixelsRead] = pixel
			s.previousPixel = pixel
			idx += 1
			pixelsRead += 1

		case (tag >> 6) == 2:
			// fmt.Printf("idx %d has 'qoi_op_luma' chunk -  tag %08b - %d ", idx, tag, tag>>6)

			pixel := pixel{A: s.previousPixel.A}

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
			// fmt.Printf("idx %d has 'qoi_op_run' chunk -  tag %08b - RUN - %d \n", idx, tag, runLength)
			if pixelsRead == 0 {
				s.historyBuffer[s.previousPixel.Hash()] = s.previousPixel // https://github.com/phoboslab/qoi/issues/258
			}
			for rIdx := pixelsRead; rIdx < pixelsRead+runLength; rIdx++ {
				s.Raw[rIdx] = pixel{R: s.previousPixel.R, G: s.previousPixel.G, B: s.previousPixel.B, A: s.previousPixel.A}
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

func ImageDecode(r io.Reader) (image.Image, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	state, err := Decode(data)
	if err != nil {
		return nil, err
	}

	img := image.NewNRGBA(image.Rect(0, 0, int(state.Width), int(state.Height)))
	for idx, pixel := range state.Raw {
		img.Set(idx%int(state.Width), idx/int(state.Width), color.NRGBA{
			R: pixel.R,
			G: pixel.G,
			B: pixel.B,
			A: pixel.A,
		})
	}
	return img, nil
}

func DecodeConfig(r io.Reader) (image.Config, error) {
	buffer := make([]byte, 14)
	n, err := r.Read(buffer)
	if err != nil || n != 14 {
		return image.Config{}, err
	}

	header, err := readHeader(buffer)
	if err != nil {
		return image.Config{}, err
	}
	
	return image.Config{
		Height: int(header.Height),
		Width:  int(header.Width),
		ColorModel: color.RGBAModel ,
	}, nil
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
	fmt.Printf("%v\n", qoiState.header)

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
