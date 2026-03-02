// Package pdf provides PDF rendering utilities for the vision-based grading
// pipeline. Instead of extracting text (OCR), it renders each page of a PDF
// to a PNG image that is passed directly to the Vision-LLM.
package pdf

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"sync"

	"github.com/gen2brain/go-fitz"
)

// Renderer converts a PDF binary blob into a slice of PNG-encoded page images.
// It is safe for concurrent use.
type Renderer struct {
	// DPI controls rendering resolution. 150 DPI is sufficient for typed text;
	// use 200–300 for dense handwriting.
	DPI float64
	// MaxPages caps how many pages are rendered (0 = all pages).
	MaxPages int
}

// NewRenderer creates a Renderer with sensible defaults.
// 200 DPI keeps A4 pages at ~1654×2339 px — optimised for handwritten
// answer sheets while staying within payload limits when JPEG-compressed.
func NewRenderer() *Renderer {
	return &Renderer{
		DPI:      200,
		MaxPages: 0,
	}
}

// RenderPages converts every page of pdfData to a PNG byte slice.
// Each element of the returned slice corresponds to one page, in order.
func (r *Renderer) RenderPages(pdfData []byte) ([][]byte, error) {
	if len(pdfData) == 0 {
		return nil, fmt.Errorf("empty pdf data")
	}

	doc, err := fitz.NewFromMemory(pdfData)
	if err != nil {
		return nil, fmt.Errorf("failed to open pdf: %w", err)
	}
	defer doc.Close()

	totalPages := doc.NumPage()
	limit := totalPages
	if r.MaxPages > 0 && r.MaxPages < totalPages {
		limit = r.MaxPages
	}

	pageImages := make([][]byte, limit)
	errs := make([]error, limit)
	var wg sync.WaitGroup

	for i := 0; i < limit; i++ {
		wg.Add(1)
		go func(pageNum int) {
			defer wg.Done()

			// fitz.Document is not thread-safe, so each goroutine needs its own instance
			localDoc, err := fitz.NewFromMemory(pdfData)
			if err != nil {
				errs[pageNum] = fmt.Errorf("failed to open pdf for page %d: %w", pageNum+1, err)
				return
			}
			defer localDoc.Close()

			// ImageDPI renders the page at the specified DPI so we control output size.
			// Using the bare Image() call renders at the PDF's native resolution, which
			// for scanned documents can be 300+ DPI and produces multi-MB images.
			img, err := localDoc.ImageDPI(pageNum, r.DPI)
			if err != nil {
				errs[pageNum] = fmt.Errorf("failed to render pdf page %d: %w", pageNum+1, err)
				return
			}

			// Pre-process: enhance contrast (makes faded ink/pencil clearer)
			// then sharpen edges (crisper character boundaries for the vision model).
			processed := enhanceContrast(img, 1.4)
			processed = sharpen(processed)

			var buf bytes.Buffer
			// JPEG at quality 85 balances file size with readability for handwritten text,
			// keeping the total request payload well under OpenRouter's gateway limit.
			if err := jpeg.Encode(&buf, processed, &jpeg.Options{Quality: 85}); err != nil {
				errs[pageNum] = fmt.Errorf("failed to encode page %d as jpeg: %w", pageNum+1, err)
				return
			}

			pageImages[pageNum] = buf.Bytes()
		}(i)
	}

	wg.Wait()

	// Check for any errors during concurrent rendering
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}

	if len(pageImages) == 0 {
		return nil, fmt.Errorf("pdf has no renderable pages")
	}

	return pageImages, nil
}

// ---------------------------------------------------------------------------
// Image pre-processing helpers for scanned handwritten answer sheets
// ---------------------------------------------------------------------------

// enhanceContrast applies a linear contrast stretch around the midpoint.
// factor > 1.0 increases contrast; 1.0 is identity.
// Makes faded pencil/pen strokes more visible to the vision model.
func enhanceContrast(src image.Image, factor float64) *image.RGBA {
	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := src.At(x, y).RGBA()
			// RGBA() returns 16-bit pre-multiplied values; shift to 8-bit.
			rf := float64(r>>8) / 255.0
			gf := float64(g>>8) / 255.0
			bf := float64(b>>8) / 255.0

			// Contrast around midpoint 0.5
			rf = clamp01((rf-0.5)*factor + 0.5)
			gf = clamp01((gf-0.5)*factor + 0.5)
			bf = clamp01((bf-0.5)*factor + 0.5)

			dst.SetRGBA(x, y, color.RGBA{
				R: uint8(rf * 255),
				G: uint8(gf * 255),
				B: uint8(bf * 255),
				A: uint8(a >> 8),
			})
		}
	}
	return dst
}

// sharpen applies a 3×3 centre-weighted laplacian kernel to crisp up
// handwritten character edges.  Kernel:
//
//	[  0  -1   0 ]
//	[ -1   5  -1 ]
//	[  0  -1   0 ]
func sharpen(src image.Image) *image.RGBA {
	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)
	w, h := bounds.Dx(), bounds.Dy()

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			// Border pixels: copy as-is to avoid boundary checks.
			if x == bounds.Min.X || x == bounds.Min.X+w-1 ||
				y == bounds.Min.Y || y == bounds.Min.Y+h-1 {
				dst.Set(x, y, src.At(x, y))
				continue
			}

			var rr, gg, bb float64
			for _, k := range [][3]int{
				{0, -1, -1}, // top
				{-1, 0, -1}, // left
				{0, 0, 5},   // centre
				{1, 0, -1},  // right
				{0, 1, -1},  // bottom
			} {
				r, g, b, _ := src.At(x+k[0], y+k[1]).RGBA()
				f := float64(k[2])
				rr += float64(r>>8) * f
				gg += float64(g>>8) * f
				bb += float64(b>>8) * f
			}

			_, _, _, a := src.At(x, y).RGBA()
			dst.SetRGBA(x, y, color.RGBA{
				R: uint8(math.Min(math.Max(rr, 0), 255)),
				G: uint8(math.Min(math.Max(gg, 0), 255)),
				B: uint8(math.Min(math.Max(bb, 0), 255)),
				A: uint8(a >> 8),
			})
		}
	}
	return dst
}

// clamp01 restricts v to the [0, 1] range.
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
