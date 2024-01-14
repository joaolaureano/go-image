package main

import (
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"sync"
)

type PixelCoordinates struct {
	MaxPixel [2]int
	MinPixel [2]int
}

func main() {
	img := openImage("./download.jpeg")
	p := distributePixels(img.Bounds(), 1)
	var wg sync.WaitGroup
	wg.Add(len(p))
	result := image.NewRGBA(img.Bounds())
	for _, pixels := range p {
		go removeComponentFilter(result, pixels, &wg)
	}
	wg.Wait()

	noRedFile, err := os.Create("output_noRkkk.jpg")
	if err != nil {
		fmt.Println("Error creating grayscale output image:", err)
		return
	}
	defer noRedFile.Close()
	jpeg.Encode(noRedFile, result, nil)

}

func openImage(filename string) image.Image {
	file, err := os.Open(filename)
	if err != nil {
		panic(fmt.Sprintf("Error opening image: %s", err.Error()))
	}
	defer file.Close()
	// Decode the image
	img, _, err := image.Decode(file)
	if err != nil {
		panic(fmt.Sprintf("Error decoding image: %s", err.Error()))
		return nil
	}
	return img

}

//	func applyGrayscaleFilter(img image.Image) image.Image {
//		bounds := img.Bounds()
//		result := image.NewRGBA(bounds)
//
//		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
//			for x := bounds.Min.X; x < bounds.Max.X; x++ {
//				pixel := img.At(x, y)
//				originalColor := color.RGBAModel.Convert(pixel).(color.RGBA)
//				grayValue := (originalColor.R + originalColor.G + originalColor.B) / 3
//				grayPixel := color.RGBA{R: grayValue, G: grayValue, B: grayValue, A: originalColor.A}
//				result.Set(x, y, grayPixel)
//			}
//		}
//
//		return result
//	}
func removeComponentFilter(img *image.RGBA, coordinates PixelCoordinates, wg *sync.WaitGroup) {
	defer wg.Done()
	maxPixel := coordinates.MaxPixel
	minPixel := coordinates.MinPixel

	for y := minPixel[1]; y <= maxPixel[1]; y++ {
		for x := minPixel[0]; x <= maxPixel[0]; x++ {
			pixel := (*img).At(x, y)
			originalColor := color.RGBAModel.Convert(pixel).(color.RGBA)
			originalColor.G = 0
			img.Set(x, y, originalColor)
		}
	}

}
func distributePixels(image image.Rectangle, n int) []PixelCoordinates {
	x := image.Max.X
	y := image.Max.Y
	result := make([]PixelCoordinates, n)
	remainders := x * y % n
	chunkSize := x * y / n
	firstValue := 0
	endValue := 0
	for i := 0; i < n-1; i++ {
		result[i].MinPixel = findValuePosition(x, y, firstValue)
		endValue += chunkSize
		if remainders > 0 {
			remainders--
			endValue++
		}
		result[i].MaxPixel = findValuePosition(x, y, endValue)
		firstValue = endValue + 1
	}
	result[len(result)-1] = PixelCoordinates{
		MaxPixel: [2]int{x - 1, y - 1},
		MinPixel: findValuePosition(x, y, firstValue),
	}

	return result
}
func findValuePosition(x, y, value int) [2]int {
	if value > x*y {
		return [2]int{x - 1, y - 1} // Return the last position if value is out of bounds
	}
	if value == 0 {
		return [2]int{0, 0}
	}
	return [2]int{
		(value - 1) / y,
		(value - 1) % y,
	}

}
