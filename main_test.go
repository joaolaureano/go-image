package main

import (
	"image"
	"image/color"
	"image/draw"
	"runtime"
	"sync"
	"testing"
)

// These tests pin the three defects that were found by profiling this package,
// so that an optimization cannot quietly reintroduce any of them. Each one
// failed before the fix.

// TestPartitionCoversEveryPixelExactlyOnce guards the partitioning. The old
// implementation derived rectangle corners from a linear pixel index, which
// left 49.6% of the image untouched at n=4 and 74.5% at n=8.
func TestPartitionCoversEveryPixelExactlyOnce(t *testing.T) {
	bounds := image.Rect(0, 0, 274, 184)
	for _, n := range []int{1, 2, 3, 4, 8, 16, 200, 1000} {
		seen := make(map[[2]int]int, bounds.Dx()*bounds.Dy())
		for _, p := range distributePixels(bounds, n) {
			for y := p.MinPixel[1]; y <= p.MaxPixel[1]; y++ {
				for x := p.MinPixel[0]; x <= p.MaxPixel[0]; x++ {
					seen[[2]int{x, y}]++
				}
			}
		}
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				if got := seen[[2]int{x, y}]; got != 1 {
					t.Fatalf("n=%d: pixel (%d,%d) visited %d times, want exactly 1", n, x, y, got)
				}
			}
		}
		if len(seen) != bounds.Dx()*bounds.Dy() {
			t.Errorf("n=%d: partition visits %d pixels, image has %d", n, len(seen), bounds.Dx()*bounds.Dy())
		}
	}
}

// TestFilterReadsTheSourceImage guards the defect that made the program useless:
// the filter was handed only the freshly allocated destination, so it read
// zeros and wrote zeros, and every output image was solid black.
func TestFilterReadsTheSourceImage(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := 0; i < len(src.Pix); i += 4 {
		src.Pix[i], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3] = 200, 150, 100, 255
	}
	dst := image.NewRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)

	var wg sync.WaitGroup
	parts := distributePixels(dst.Bounds(), 4)
	wg.Add(len(parts))
	for _, p := range parts {
		go removeComponentFilter(dst, p, &wg)
	}
	wg.Wait()

	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			got := dst.RGBAAt(x, y)
			want := color.RGBA{R: 200, G: 0, B: 100, A: 255}
			if got != want {
				t.Fatalf("pixel (%d,%d) = %v, want %v", x, y, got, want)
			}
		}
	}
}

// TestGrayscaleDoesNotOverflow guards the uint8 wraparound. Summing three
// uint8 channels overflows at 256 before the division, so white came out as
// gray 84; 31% of download.jpeg's pixels were wrong.
func TestGrayscaleDoesNotOverflow(t *testing.T) {
	cases := []struct {
		in   color.RGBA
		want uint8
	}{
		{color.RGBA{255, 255, 255, 255}, 255},
		{color.RGBA{200, 200, 200, 255}, 200},
		{color.RGBA{0, 0, 0, 255}, 0},
		{color.RGBA{255, 0, 0, 255}, 85},
		{color.RGBA{10, 20, 30, 255}, 20},
	}
	for _, c := range cases {
		src := image.NewRGBA(image.Rect(0, 0, 1, 1))
		src.SetRGBA(0, 0, c.in)
		applyGrayscaleFilterInPlace(src)
		got := src.RGBAAt(0, 0)
		if got.R != c.want || got.G != c.want || got.B != c.want {
			t.Errorf("gray(%v) = %v, want all channels %d", c.in, got, c.want)
		}
		if got.A != c.in.A {
			t.Errorf("gray(%v) alpha = %d, want %d", c.in, got.A, c.in.A)
		}
	}
}

// TestConcurrencyDoesNotChangeTheResult pins that the goroutine count is a
// performance decision and nothing else.
func TestConcurrencyDoesNotChangeTheResult(t *testing.T) {
	src := openImage("./download.jpeg")
	bounds := src.Bounds()
	run := func(n int) *image.RGBA {
		dst := image.NewRGBA(bounds)
		drawSrc(dst, src)
		parts := distributePixels(bounds, n)
		var wg sync.WaitGroup
		wg.Add(len(parts))
		for _, p := range parts {
			go removeComponentFilter(dst, p, &wg)
		}
		wg.Wait()
		return dst
	}
	want := run(1)
	for _, n := range []int{2, 4, 8, runtime.NumCPU()} {
		got := run(n)
		for i := range want.Pix {
			if want.Pix[i] != got.Pix[i] {
				t.Fatalf("n=%d differs from n=1 at byte %d: %d vs %d", n, i, got.Pix[i], want.Pix[i])
			}
		}
	}
}

// TestGreenIsRemovedFromRealImage is the end-to-end property: green gone,
// red and blue untouched.
func TestGreenIsRemovedFromRealImage(t *testing.T) {
	src := openImage("./download.jpeg")
	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)
	drawSrc(dst, src)

	reference := image.NewRGBA(bounds)
	drawSrc(reference, src)

	parts := distributePixels(bounds, 8)
	var wg sync.WaitGroup
	wg.Add(len(parts))
	for _, p := range parts {
		go removeComponentFilter(dst, p, &wg)
	}
	wg.Wait()

	nonZeroRed := 0
	for i := 0; i < len(dst.Pix); i += 4 {
		if dst.Pix[i+1] != 0 {
			t.Fatalf("green channel not cleared at byte %d", i+1)
		}
		if dst.Pix[i] != reference.Pix[i] || dst.Pix[i+2] != reference.Pix[i+2] {
			t.Fatalf("red or blue changed at byte %d", i)
		}
		if dst.Pix[i] != 0 {
			nonZeroRed++
		}
	}
	// The old code produced an all-zero image; this asserts the output
	// actually carries the source.
	if nonZeroRed == 0 {
		t.Error("output has no red anywhere; the filter is not reading the source")
	}
}

// drawSrc copies src into dst, which is what main does before filtering.
func drawSrc(dst *image.RGBA, src image.Image) {
	draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Src)
}

// TestGrayscaleIsIdempotent pins that averaging three already-equal channels
// is a no-op, which is what makes the in-place form safe to run twice.
func TestGrayscaleIsIdempotent(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = uint8(i), uint8(i*3), uint8(i*7), 255
	}
	applyGrayscaleFilterInPlace(img)
	once := append([]uint8(nil), img.Pix...)
	applyGrayscaleFilterInPlace(img)
	for i := range once {
		if once[i] != img.Pix[i] {
			t.Fatalf("second pass changed byte %d: %d -> %d", i, once[i], img.Pix[i])
		}
	}
}

// TestFiltersDoNotAllocate is the property this package now claims. It fails if
// an interface round trip creeps back into either hot loop.
func TestFiltersDoNotAllocate(t *testing.T) {
	bounds := image.Rect(0, 0, 64, 64)
	dst := image.NewRGBA(bounds)
	coords := distributePixels(bounds, 1)[0]
	var wg sync.WaitGroup

	if got := testing.AllocsPerRun(50, func() {
		wg.Add(1)
		removeComponentFilter(dst, coords, &wg)
		wg.Wait()
	}); got != 0 {
		t.Errorf("removeComponentFilter allocates %.1f times per call, want 0", got)
	}

	if got := testing.AllocsPerRun(50, func() {
		applyGrayscaleFilterInPlace(dst)
	}); got != 0 {
		t.Errorf("applyGrayscaleFilterInPlace allocates %.1f times per call, want 0", got)
	}
}

// --- blur ---------------------------------------------------------------

func solid(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	return img
}

// TestBlurOfUniformImageIsUnchanged is the sanity property: averaging equal
// values returns the same value, at the edges too. It is the test that catches
// a wrong divisor or broken edge clamping.
func TestBlurOfUniformImageIsUnchanged(t *testing.T) {
	for _, radius := range []int{1, 2, 5, 17} {
		src := solid(23, 19, color.RGBA{40, 80, 120, 255})
		got := applyBlurFilter(src, radius)
		for i := range got.Pix {
			if got.Pix[i] != src.Pix[i] {
				t.Fatalf("radius=%d: byte %d = %d, want %d", radius, i, got.Pix[i], src.Pix[i])
			}
		}
	}
}

// TestBlurRadiusZeroIsIdentity pins the documented no-op.
func TestBlurRadiusZeroIsIdentity(t *testing.T) {
	src := openImage("./download.jpeg")
	rgba := image.NewRGBA(src.Bounds())
	drawSrc(rgba, src)
	got := applyBlurFilter(rgba, 0)
	for i := range got.Pix {
		if got.Pix[i] != rgba.Pix[i] {
			t.Fatalf("radius=0 changed byte %d", i)
		}
	}
}

// TestBlurSpreadsAndConserves checks the two things a blur must do: energy
// moves outward from a bright point, and the image gets no brighter overall.
func TestBlurSpreadsAndConserves(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 21, 21))
	for i := 0; i < len(src.Pix); i += 4 {
		src.Pix[i+3] = 255
	}
	src.SetRGBA(10, 10, color.RGBA{255, 255, 255, 255})

	got := applyBlurFilter(src, 3)

	if c := got.RGBAAt(10, 10); c.R >= 255 {
		t.Errorf("center still saturated (%d); the point did not spread", c.R)
	}
	if c := got.RGBAAt(12, 10); c.R == 0 {
		t.Error("neighbour got no light; the blur is not spreading")
	}
	if c := got.RGBAAt(0, 0); c.R != 0 {
		t.Errorf("far corner brightened to %d; the window reaches too far", c.R)
	}
	var before, after int
	for i := 0; i < len(src.Pix); i += 4 {
		before += int(src.Pix[i])
		after += int(got.Pix[i])
	}
	if after > before {
		t.Errorf("blur created light: %d -> %d", before, after)
	}
}

// TestBlurIsSymmetric pins that a symmetric input stays symmetric, which a
// sliding window with an off-by-one in either direction would break.
func TestBlurIsSymmetric(t *testing.T) {
	const n = 15
	src := image.NewRGBA(image.Rect(0, 0, n, n))
	for i := 0; i < len(src.Pix); i += 4 {
		src.Pix[i+3] = 255
	}
	src.SetRGBA(n/2, n/2, color.RGBA{200, 200, 200, 255})

	got := applyBlurFilter(src, 4)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			if a, b := got.RGBAAt(x, y), got.RGBAAt(n-1-x, y); a != b {
				t.Fatalf("not symmetric horizontally at (%d,%d): %v vs %v", x, y, a, b)
			}
			if a, b := got.RGBAAt(x, y), got.RGBAAt(x, n-1-y); a != b {
				t.Fatalf("not symmetric vertically at (%d,%d): %v vs %v", x, y, a, b)
			}
		}
	}
}

// TestBlurIntoDoesNotAllocate is the claim that makes BlurInto worth exposing:
// given scratch, repeated blurs cost nothing in allocations.
func TestBlurIntoDoesNotAllocate(t *testing.T) {
	src := solid(64, 64, color.RGBA{10, 20, 30, 255})
	dst := image.NewRGBA(src.Bounds())
	scratch := make([]uint8, len(src.Pix))
	if got := testing.AllocsPerRun(20, func() {
		BlurInto(dst, src, 3, scratch)
	}); got != 0 {
		t.Errorf("BlurInto allocates %.1f times per call, want 0", got)
	}
}

// TestBlurIntoRejectsTooSmallScratch pins the panic rather than letting it
// corrupt memory silently.
func TestBlurIntoRejectsTooSmallScratch(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("BlurInto accepted a scratch buffer that was too small")
		}
	}()
	src := solid(8, 8, color.RGBA{1, 2, 3, 255})
	BlurInto(image.NewRGBA(src.Bounds()), src, 2, make([]uint8, 4))
}
