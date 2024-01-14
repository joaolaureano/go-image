package main

import (
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"runtime"
	"sync"
	"time"
)

var numOfThreads = runtime.NumCPU()

func main() {
	start := time.Now()
	defer func() {
		fmt.Printf("took %v\n", time.Since(start))
	}()
	img := openImage("./test_images/download.jpeg")
	imgBounds := img.Bounds()
	p := distributePixels(imgBounds.Max.Y, numOfThreads)
	var wg sync.WaitGroup
	wg.Add(len(p))
	result := image.NewRGBA(imgBounds)
	for _, rows := range p {
		go removeComponentFilter(img, result, rows, &wg)
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
func removeComponentFilter(image image.Image, img *image.RGBA, rows []int, wg *sync.WaitGroup) {
	defer wg.Done()

	for y := rows[0]; y <= rows[1]; y++ {
		for x := img.Bounds().Min.X; x <= img.Bounds().Max.X; x++ {
			originalColor := color.RGBAModel.Convert((image).At(x, y)).(color.RGBA)
			originalColor.G = 0
			img.Set(x, y, originalColor)
		}
	}
}
func distributePixels(imageHeight int, n int) [][]int {
	y := imageHeight
	if y < n {
		return [][]int{{0, y}}
	}
	result := make([][]int, n)

	for i := 0; i < n; i++ {
		startY := (i * y) / n
		endY := ((i + 1) * y) / n
		result[i] = []int{startY, endY - 1}
	}
	return result
}
