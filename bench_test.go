package main

import (
	"fmt"
	"image"
	"image/draw"
	"sync"
	"testing"
)

// Benchmarks for the per-pixel filters. The work is one pass over every pixel
// of download.jpeg (274x184, ~50k pixels) and is entirely CPU-bound: the decode
// happens once, outside the timed loop, so nothing here touches the disk while
// measuring.
//
// Setup is hoisted out of the loop wherever the operation allows it. An earlier
// version allocated the destination image on every iteration, which meant
// image.NewRGBA's two allocations were being attributed to the filter — the
// benchmark reported 3 allocs/op for a filter that performs none.
//
// The b.N form is used rather than b.Loop because go.mod declares go 1.21.

func benchImage(b *testing.B) image.Image {
	b.Helper()
	return openImage("./download.jpeg")
}

// rgbaCopy is what main works on: the source materialized into an RGBA buffer.
func rgbaCopy(b *testing.B, src image.Image) *image.RGBA {
	b.Helper()
	dst := image.NewRGBA(src.Bounds())
	draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Src)
	return dst
}

// BenchmarkRemoveComponentFilter times the filter alone, on a buffer reused
// across iterations. Reuse is sound here because clearing a channel is
// idempotent: the second pass over an already-filtered buffer does the same
// work as the first.
func BenchmarkRemoveComponentFilter(b *testing.B) {
	dst := rgbaCopy(b, benchImage(b))
	coords := distributePixels(dst.Bounds(), 1)[0]
	var wg sync.WaitGroup
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wg.Add(1)
		removeComponentFilter(dst, coords, &wg)
		wg.Wait()
	}
}

// BenchmarkPipeline8 is how main runs it: the same work split across
// goroutines. Kept separate so a change cannot look good on one shape while
// quietly taxing the other. The allocations it reports are the goroutines'
// stacks and the WaitGroup, not the filter.
func BenchmarkPipeline8(b *testing.B) {
	dst := rgbaCopy(b, benchImage(b))
	parts := distributePixels(dst.Bounds(), 8)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		wg.Add(len(parts))
		for _, p := range parts {
			go removeComponentFilter(dst, p, &wg)
		}
		wg.Wait()
	}
}

// BenchmarkApplyGrayscaleFilterInPlace is the allocation-free form.
//
// Unlike clearing a channel, grayscale is NOT idempotent — averaging three
// equal channels returns the same value, so a second pass is a no-op rather
// than the same work. The buffer is therefore restored from a pristine copy
// each iteration, with the restore excluded from the timing.
func BenchmarkApplyGrayscaleFilterInPlace(b *testing.B) {
	src := rgbaCopy(b, benchImage(b))
	dst := image.NewRGBA(src.Bounds())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		copy(dst.Pix, src.Pix)
		b.StartTimer()
		applyGrayscaleFilterInPlace(dst)
	}
}

// BenchmarkBlurInto times the blur with scratch supplied by the caller, which
// is the allocation-free path. The radius is varied to show that a separable
// blur with a sliding window costs the same regardless: each output pixel is a
// constant amount of work no matter how wide the window is.
func BenchmarkBlurInto(b *testing.B) {
	src := rgbaCopy(b, benchImage(b))
	dst := image.NewRGBA(src.Bounds())
	scratch := make([]uint8, len(src.Pix))
	for _, radius := range []int{1, 4, 16} {
		b.Run(fmt.Sprintf("radius%d", radius), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				BlurInto(dst, src, radius, scratch)
			}
		})
	}
}

// BenchmarkApplyBlurFilter is the convenience form, which allocates the result
// and the scratch. It exists to show what that convenience costs.
func BenchmarkApplyBlurFilter(b *testing.B) {
	src := rgbaCopy(b, benchImage(b))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink = applyBlurFilter(src, 4)
	}
}

// sink keeps the compiler from deciding a filter result is unused and
// eliminating the call the benchmark exists to time.
var sink image.Image

// BenchmarkDistributePixels covers the partitioning arithmetic on its own; it
// runs once per image in main, so it should be nowhere near the hot path and
// this benchmark exists to confirm that rather than to optimize it.
func BenchmarkDistributePixels(b *testing.B) {
	bounds := benchImage(b).Bounds()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		distributePixels(bounds, 8)
	}
}
