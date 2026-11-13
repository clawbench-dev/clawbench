package kb

import (
	"fmt"
	"strings"

	"github.com/ledongthuc/pdf"
)

// extractPDF emits one block per page. It is deliberately tolerant: a malformed
// page is skipped with a warning rather than failing the whole document, and the
// library's panics on broken cross-reference tables are recovered per page.
func extractPDF(path string) (blocks []Block, err error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return nil, &FileError{Reason: ReasonPDFEncrypted, Err: err}
	}
	defer func() { _ = f.Close() }()

	n := r.NumPage()
	if n == 0 {
		return nil, &FileError{Reason: ReasonPDFNoText, Err: ErrNoText}
	}
	for i := 1; i <= n; i++ {
		text, pageErr := pdfPageText(r, i)
		if pageErr != nil {
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		blocks = append(blocks, Block{Text: text, Location: Location{Kind: "page", Start: i}})
	}
	if len(blocks) == 0 {
		return nil, &FileError{Reason: ReasonPDFNoText, Err: ErrNoText}
	}
	return blocks, nil
}

func pdfPageText(r *pdf.Reader, pageNum int) (text string, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("PDF page %d parse failed", pageNum)
		}
	}()
	p := r.Page(pageNum)
	if p.V.IsNull() {
		return "", nil
	}
	return p.GetPlainText(nil)
}
