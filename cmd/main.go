package main

import (
	"fmt"
	"log"
	"os"

	"github.com/kriticalflare/qoi"
)

func main() {
	file, err := os.Open("./testimages/dice.qoi")
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()
	img, err := qoi.ImageDecode(file)
	if err != nil {
		log.Fatalf("error with image decode: %v", err)
	}
	// fmt.Printf("img %v, \n", img)
	fmt.Printf("bounds %v", img.Bounds())

	file, err = os.Create("./output/image.qoi")
	if err != nil {
		log.Fatalf("failed to open write file: %v", err)
	}

	qoi.ImageEncode(file, img)
}

