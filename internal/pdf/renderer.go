// Package pdf provides PDF rendering utilities for the vision-based grading
// pipeline. Instead of extracting text (OCR), it renders each page of a PDF
// to a PNG image that is passed directly to the Vision-LLM.
package pdf

import (
	"bytes"
	"fmt"
	"image/jpeg"

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
// 120 DPI keeps A4 pages at ~990×1400 px — enough for LLM reading, small enough
// to avoid 502s from oversized payloads when encoded as JPEG.
func NewRenderer() *Renderer {
	return &Renderer{
		DPI:      120,
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

	pageImages := make([][]byte, 0, limit)

	for i := 0; i < limit; i++ {
		// ImageDPI renders the page at the specified DPI so we control output size.
		// Using the bare Image() call renders at the PDF's native resolution, which
		// for scanned documents can be 300+ DPI and produces multi-MB images.
		img, err := doc.ImageDPI(i, r.DPI)
		if err != nil {
			return nil, fmt.Errorf("failed to render pdf page %d: %w", i+1, err)
		}

		var buf bytes.Buffer
		// JPEG at quality 75 is ~10-20x smaller than lossless PNG for scanned pages,
		// keeping the total request payload well under OpenRouter's gateway limit.
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 75}); err != nil {
			return nil, fmt.Errorf("failed to encode page %d as jpeg: %w", i+1, err)
		}

		pageImages = append(pageImages, buf.Bytes())
	}

	if len(pageImages) == 0 {
		return nil, fmt.Errorf("pdf has no renderable pages")
	}

	return pageImages, nil
}
