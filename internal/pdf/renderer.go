// Package pdf provides PDF rendering utilities for the vision-based grading
// pipeline. Instead of extracting text (OCR), it renders each page of a PDF
// to a PNG image that is passed directly to the Vision-LLM.
package pdf

import (
	"bytes"
	"fmt"
	"image/jpeg"
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
// 150 DPI keeps A4 pages at ~1240×1754 px — sharp enough for handwriting
// recognition while staying within payload limits when JPEG-compressed.
func NewRenderer() *Renderer {
	return &Renderer{
		DPI:      150,
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

			var buf bytes.Buffer
			// JPEG at quality 85 balances file size with readability for handwritten text,
			// keeping the total request payload well under OpenRouter's gateway limit.
			if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
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
