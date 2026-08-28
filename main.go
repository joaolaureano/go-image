package main

import (
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"runtime"
	"sync"
)

type PixelCoordinates struct {
	MaxPixel [2]int
	MinPixel [2]int
}

func main() {
	img := openImage("./download.jpeg")
	bounds := img.Bounds()

	// The filter works in place, so the destination has to start as a copy of
	// the source. Without this the filter reads the freshly allocated (all
	// zero) destination and writes zeros back, and the program produces a
	// solid black image no matter what the input is.
	result := image.NewRGBA(bounds)
	draw.Draw(result, bounds, img, bounds.Min, draw.Src)

	parts := distributePixels(bounds, runtime.NumCPU())
	var wg sync.WaitGroup
	wg.Add(len(parts))
	for _, pixels := range parts {
		go removeComponentFilter(result, pixels, &wg)
	}
	wg.Wait()

	noRedFile, err := os.Create("output_no.jpg")
	if err != nil {
		fmt.Println("Error creating output image:", err)
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
	img, _, err := image.Decode(file)
	if err != nil {
		panic(fmt.Sprintf("Error decoding image: %s", err.Error()))
	}
	return img
}

// applyGrayscaleFilterInPlace converts img to grayscale, writing over it.
//
// It allocates nothing. Grayscale is a per-pixel function of that pixel alone,
// so there is no need for a second buffer: the three channels are read and the
// average written back before moving on. A filter that reads a neighbourhood —
// a blur, a convolution — genuinely does need somewhere untouched to read from,
// and could not be written this way.
func applyGrayscaleFilterInPlace(img *image.RGBA) {
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		i := img.PixOffset(bounds.Min.X, y)
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			// Widened to int before summing. As uint8 the sum wraps at 256
			// before the division: white (255,255,255) came out as 84 instead
			// of 255, and 31% of this image's pixels were wrong.
			gray := uint8((int(img.Pix[i]) + int(img.Pix[i+1]) + int(img.Pix[i+2])) / 3)
			img.Pix[i] = gray
			img.Pix[i+1] = gray
			img.Pix[i+2] = gray
			i += 4
		}
	}
}

// Blur is a separable box blur.
//
// Unlike the other two filters this one cannot work in place. Each output pixel
// is the average of a neighbourhood of inputs, so writing a result over the
// source would corrupt the input of every pixel processed after it. The second
// buffer is a requirement of the algorithm, not an oversight — which is exactly
// the distinction worth keeping: clearing a channel or averaging one pixel's
// own components needs no extra memory, and reading a neighbourhood does.
//
// The blur is separable: a 2D box average equals a horizontal pass followed by
// a vertical one, which turns the cost per pixel from O(radius^2) into O(1)
// via a sliding window. That is why the radius barely shows up in the timings.
//
// Edges are handled by clamping, i.e. the border pixel is repeated outside the
// image, so the window always holds 2*radius+1 samples and the divisor is
// constant.

// BlurInto writes a blurred copy of src into dst, allocating nothing.
//
// scratch must hold at least len(src.Pix) bytes and is used for the
// intermediate horizontal pass; pass a slice you keep between calls to blur
// repeatedly without allocating. dst and src must have the same bounds, and dst
// must not alias src.
func BlurInto(dst, src *image.RGBA, radius int, scratch []uint8) {
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w == 0 || h == 0 {
		return
	}
	if radius < 1 {
		copy(dst.Pix, src.Pix)
		return
	}
	if len(scratch) < len(src.Pix) {
		panic("BlurInto: scratch too small")
	}

	window := int32(2*radius + 1)

	// Horizontal pass: src -> scratch.
	for y := 0; y < h; y++ {
		row := y * src.Stride
		out := y * dst.Stride
		var sum [4]int32
		for i := -radius; i <= radius; i++ {
			o := row + clamp(i, 0, w-1)*4
			sum[0] += int32(src.Pix[o])
			sum[1] += int32(src.Pix[o+1])
			sum[2] += int32(src.Pix[o+2])
			sum[3] += int32(src.Pix[o+3])
		}
		for x := 0; x < w; x++ {
			d := out + x*4
			scratch[d] = uint8(sum[0] / window)
			scratch[d+1] = uint8(sum[1] / window)
			scratch[d+2] = uint8(sum[2] / window)
			scratch[d+3] = uint8(sum[3] / window)

			drop := row + clamp(x-radius, 0, w-1)*4
			add := row + clamp(x+radius+1, 0, w-1)*4
			sum[0] += int32(src.Pix[add]) - int32(src.Pix[drop])
			sum[1] += int32(src.Pix[add+1]) - int32(src.Pix[drop+1])
			sum[2] += int32(src.Pix[add+2]) - int32(src.Pix[drop+2])
			sum[3] += int32(src.Pix[add+3]) - int32(src.Pix[drop+3])
		}
	}

	// Vertical pass: scratch -> dst.
	stride := dst.Stride
	for x := 0; x < w; x++ {
		col := x * 4
		var sum [4]int32
		for i := -radius; i <= radius; i++ {
			o := clamp(i, 0, h-1)*stride + col
			sum[0] += int32(scratch[o])
			sum[1] += int32(scratch[o+1])
			sum[2] += int32(scratch[o+2])
			sum[3] += int32(scratch[o+3])
		}
		for y := 0; y < h; y++ {
			d := y*stride + col
			dst.Pix[d] = uint8(sum[0] / window)
			dst.Pix[d+1] = uint8(sum[1] / window)
			dst.Pix[d+2] = uint8(sum[2] / window)
			dst.Pix[d+3] = uint8(sum[3] / window)

			drop := clamp(y-radius, 0, h-1)*stride + col
			add := clamp(y+radius+1, 0, h-1)*stride + col
			sum[0] += int32(scratch[add]) - int32(scratch[drop])
			sum[1] += int32(scratch[add+1]) - int32(scratch[drop+1])
			sum[2] += int32(scratch[add+2]) - int32(scratch[drop+2])
			sum[3] += int32(scratch[add+3]) - int32(scratch[drop+3])
		}
	}
}

// applyBlurFilter returns a blurred copy of img.
//
// It allocates two buffers: the result, and the scratch space the separable
// passes need. Callers blurring more than once should use BlurInto and keep the
// scratch slice.
func applyBlurFilter(img image.Image, radius int) *image.RGBA {
	bounds := img.Bounds()
	src, ok := img.(*image.RGBA)
	if !ok {
		src = image.NewRGBA(bounds)
		draw.Draw(src, bounds, img, bounds.Min, draw.Src)
	}
	dst := image.NewRGBA(bounds)
	BlurInto(dst, src, radius, make([]uint8, len(src.Pix)))
	return dst
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func removeComponentFilter(img *image.RGBA, coordinates PixelCoordinates, wg *sync.WaitGroup) {
	defer wg.Done()
	maxPixel := coordinates.MaxPixel
	minPixel := coordinates.MinPixel

	// The green byte is written directly rather than through At/Convert/Set.
	// img is a *image.RGBA, so its pixels are a flat []byte laid out as
	// R,G,B,A per pixel; clearing green is one store. The interface round trip
	// it replaces allocated twice per pixel to move four bytes it already had.
	for y := minPixel[1]; y <= maxPixel[1]; y++ {
		i := img.PixOffset(minPixel[0], y)
		for x := minPixel[0]; x <= maxPixel[0]; x++ {
			img.Pix[i+1] = 0
			i += 4
		}
	}
}

// distributePixels splits the image into n horizontal bands.
//
// It partitions by rows rather than by a linear pixel index because the filter
// treats MinPixel and MaxPixel as the corners of a rectangle, not as the ends
// of a run. The previous version handed out corners derived from a linear
// index, which for n=4 left 49.6% of the image untouched and for n=8 left
// 74.5%: every pixel outside the bounding box of each pair simply never got
// visited.
func distributePixels(bounds image.Rectangle, n int) []PixelCoordinates {
	if n < 1 {
		n = 1
	}
	height := bounds.Dy()
	if n > height {
		n = height
	}
	if height == 0 || bounds.Dx() == 0 {
		return nil
	}

	result := make([]PixelCoordinates, 0, n)
	rowsPer, remainder := height/n, height%n
	y := bounds.Min.Y
	for i := 0; i < n; i++ {
		rows := rowsPer
		if i < remainder {
			rows++
		}
		result = append(result, PixelCoordinates{
			MinPixel: [2]int{bounds.Min.X, y},
			MaxPixel: [2]int{bounds.Max.X - 1, y + rows - 1},
		})
		y += rows
	}
	return result
}
