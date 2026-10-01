# go-image

**[Leia em português / Read this in Portuguese](README.pt-BR.md)**

Small image filters in Go, written to practise concurrency and profiling. The
program decodes `download.jpeg`, clears the green channel of every pixel in
parallel (one goroutine per CPU, each working on a horizontal band of rows) and
writes the result to `output_no.jpg`.

Uses only the standard library (`image`, `image/jpeg`, `image/png`, `image/draw`).

## Filters

| Function | What it does | Allocations |
|---|---|---|
| `removeComponentFilter` | Sets the green channel to zero, in place, on one band of rows | none |
| `applyGrayscaleFilterInPlace` | Replaces R, G and B with their average, in place | none |
| `BlurInto` | Separable box blur with a sliding window, written into `dst` using caller-supplied scratch | none |
| `applyBlurFilter` | Convenience form of the blur that allocates the result and the scratch | two buffers |
| `distributePixels` | Splits the image into `n` horizontal bands that cover every pixel exactly once | one slice |

All filters index `image.RGBA.Pix` directly instead of going through
`At()` → `Convert()` → `Set()`, which boxed a color on the heap two or three
times per pixel.

The blur is the only filter that cannot work in place: each output pixel
averages a neighbourhood, so writing over the source would corrupt the input of
the pixels that come after it. Since it is separable, its cost per pixel does
not depend on the radius. Edges are clamped (the border pixel is repeated).

## Fixed defects

Profiling the first version turned up three bugs, each one now pinned by a test:

- **Black output:** the filter received only the freshly allocated (all zero)
  destination, never the source. The destination now starts as a copy of the
  source.
- **Lost pixels:** the partitioning built rectangle corners from a linear pixel
  index; with 4 goroutines 49.6% of the image was never visited, with 8, 74.5%.
  It now splits by rows.
- **Grayscale overflow:** the sum of three `uint8` values wrapped at 256 before
  the division, so white came out as gray 84. The sum is now widened to `int`.

## Running

Requires Go 1.21+.

```sh
go run .            # reads download.jpeg, writes output_no.jpg
```

## Tests and benchmarks

```sh
go test ./...
go test -bench=. -benchmem
```

`TestFiltersDoNotAllocate` uses `testing.AllocsPerRun` so that an allocation
cannot creep back into the per-pixel loops unnoticed.

Benchmarks on `download.jpeg` (274×184), before and after removing the
per-pixel allocation:

| Benchmark | Before | After | Change |
|---|---|---|---|
| `removeComponentFilter` | 1,391,686 ns/op | 46,802 ns/op | −96.6% |
| `applyGrayscaleFilter` | 2,026,270 ns/op | 276,094 ns/op | −86.4% |
| `Pipeline8` | 459,728 ns/op | 45,334 ns/op | −90.1% |

Separable blur vs. the naive O(r²) form:

| Radius | Separable | Naive | Speedup |
|---|---|---|---|
| 1 | 536 µs | 1.1 ms | 2× |
| 4 | 383 µs | 9.4 ms | 24× |
| 16 | 406 µs | 119.4 ms | 294× |
