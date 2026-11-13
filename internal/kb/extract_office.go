package kb

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// maxOfficeXMLBytes caps each decompressed XML part. The file itself is already
// capped by MaxFileMB, but a zip can expand far beyond its compressed size.
const maxOfficeXMLBytes = 64 << 20

func findZipFile(zr *zip.ReadCloser, name string) *zip.File {
	for _, f := range zr.File {
		if f.Name == name {
			return f
		}
	}
	return nil
}

func openZipXML(f *zip.File) (io.ReadCloser, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	return struct {
		io.Reader
		io.Closer
	}{io.LimitReader(rc, maxOfficeXMLBytes), rc}, nil
}

func attrValue(attrs []xml.Attr, local string) string {
	for _, a := range attrs {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// --- Word (.docx) ---

func extractDocx(path string) ([]Block, error) { //nolint:gocyclo // streaming OOXML parser
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, &FileError{Reason: ReasonUnknownFormat, Detail: "docx", Err: err}
	}
	defer func() { _ = zr.Close() }()

	f := findZipFile(zr, "word/document.xml")
	if f == nil {
		return nil, &FileError{Reason: ReasonNoText, Err: ErrNoText}
	}
	rc, err := openZipXML(f)
	if err != nil {
		return nil, fmt.Errorf("read docx body: %w", err)
	}
	defer func() { _ = rc.Close() }()

	dec := xml.NewDecoder(rc)
	var paras []string
	var cur strings.Builder
	inText := 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// Truncated or malformed XML: keep what was parsed.
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				cur.Reset()
			case "t":
				inText++
			case "tab":
				cur.WriteString("\t")
			case "br":
				cur.WriteString("\n")
			}
		case xml.CharData:
			if inText > 0 {
				cur.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				if inText > 0 {
					inText--
				}
			case "p":
				if s := strings.TrimSpace(cur.String()); s != "" {
					paras = append(paras, s)
				}
			}
		}
	}
	if len(paras) == 0 {
		return nil, ErrNoText
	}
	return groupParagraphs(paras, 1200), nil
}

// groupParagraphs merges small paragraphs into blocks of about maxRunes so a
// document does not turn into hundreds of one-sentence chunks. The location
// label records the paragraph range the block starts in.
func groupParagraphs(paras []string, maxRunes int) []Block {
	var out []Block
	buf := make([]string, 0, len(paras))
	runes := 0
	start := 1
	for i, p := range paras {
		n := utf8.RuneCountInString(p)
		if runes > 0 && runes+n > maxRunes {
			out = append(out, Block{Text: strings.Join(buf, "\n\n"), Location: paraRange(start, i)})
			buf = nil
			runes = 0
			start = i + 1
		}
		buf = append(buf, p)
		runes += n
	}
	if len(buf) > 0 {
		out = append(out, Block{Text: strings.Join(buf, "\n\n"), Location: paraRange(start, len(paras))})
	}
	return out
}

// --- Excel (.xlsx) ---

type sheetRef struct {
	Name   string
	Target string
}

func extractXlsx(path string) ([]Block, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, &FileError{Reason: ReasonUnknownFormat, Detail: "xlsx", Err: err}
	}
	defer func() { _ = zr.Close() }()

	shared := readSharedStrings(zr)
	sheets := readSheetRefs(zr)
	if len(sheets) == 0 {
		return nil, &FileError{Reason: ReasonNoText, Err: ErrNoText}
	}

	var out []Block
	for i, sh := range sheets {
		rows, err := readSheetRows(zr, sh.Target, shared)
		if err != nil {
			continue
		}
		name := sh.Name
		if name == "" {
			name = strconv.Itoa(i + 1)
		}
		out = append(out, groupSheetRows(rows, name)...)
	}
	if len(out) == 0 {
		return nil, ErrNoText
	}
	return out, nil
}

func readSharedStrings(zr *zip.ReadCloser) []string { //nolint:gocyclo // streaming OOXML parser
	f := findZipFile(zr, "xl/sharedStrings.xml")
	if f == nil {
		return nil
	}
	rc, err := openZipXML(f)
	if err != nil {
		return nil
	}
	defer func() { _ = rc.Close() }()

	dec := xml.NewDecoder(rc)
	var out []string
	var cur strings.Builder
	inSI := false
	inT := 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) || err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "si":
				inSI = true
				cur.Reset()
			case "t":
				inT++
			}
		case xml.CharData:
			if inSI && inT > 0 {
				cur.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				if inT > 0 {
					inT--
				}
			case "si":
				out = append(out, cur.String())
				inSI = false
			}
		}
	}
	return out
}

func readSheetRefs(zr *zip.ReadCloser) []sheetRef { //nolint:gocognit,gocyclo // streaming OOXML: rels + workbook + fallback
	rels := map[string]string{}
	if f := findZipFile(zr, "xl/_rels/workbook.xml.rels"); f != nil {
		if rc, err := openZipXML(f); err == nil {
			dec := xml.NewDecoder(rc)
			for {
				tok, err := dec.Token()
				if errors.Is(err, io.EOF) || err != nil {
					break
				}
				if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "Relationship" {
					id := attrValue(se.Attr, "Id")
					target := attrValue(se.Attr, "Target")
					if id != "" && target != "" {
						rels[id] = target
					}
				}
			}
			_ = rc.Close()
		}
	}

	var sheets []sheetRef
	if f := findZipFile(zr, "xl/workbook.xml"); f != nil {
		if rc, err := openZipXML(f); err == nil {
			dec := xml.NewDecoder(rc)
			for {
				tok, err := dec.Token()
				if errors.Is(err, io.EOF) || err != nil {
					break
				}
				if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "sheet" {
					name := attrValue(se.Attr, "name")
					rid := attrValue(se.Attr, "id")
					target := rels[rid]
					if target == "" {
						continue
					}
					sheets = append(sheets, sheetRef{Name: name, Target: normalizeZipTarget(target)})
				}
			}
			_ = rc.Close()
		}
	}
	if len(sheets) > 0 {
		return sheets
	}

	// Fallback: enumerate worksheet files in numeric order.
	var names []string
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "xl/worksheets/") && strings.HasSuffix(f.Name, ".xml") {
			names = append(names, f.Name)
		}
	}
	sort.Slice(names, func(i, j int) bool { return naturalLess(names[i], names[j]) })
	for _, n := range names {
		sheets = append(sheets, sheetRef{Target: n})
	}
	return sheets
}

func normalizeZipTarget(target string) string {
	target = strings.TrimPrefix(target, "/")
	if strings.HasPrefix(target, "xl/") {
		return target
	}
	return "xl/" + target
}

func readSheetRows(zr *zip.ReadCloser, target string, shared []string) ([]string, error) { //nolint:gocognit,gocyclo // streaming OOXML parser
	f := findZipFile(zr, target)
	if f == nil {
		return nil, fmt.Errorf("sheet %s not found", target)
	}
	rc, err := openZipXML(f)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()

	dec := xml.NewDecoder(rc)
	var rows []string
	var cells []string
	var cur strings.Builder
	cellType := ""
	inV := false
	inIS := false
	inT := 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) || err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				cells = nil
			case "c":
				cellType = attrValue(t.Attr, "t")
				cur.Reset()
			case "v":
				inV = true
			case "is":
				inIS = true
			case "t":
				inT++
			}
		case xml.CharData:
			if inV || (inIS && inT > 0) {
				cur.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v":
				inV = false
			case "t":
				if inT > 0 {
					inT--
				}
			case "is":
				inIS = false
			case "c":
				val := strings.TrimSpace(cur.String())
				if cellType == "s" {
					if idx, err := strconv.Atoi(val); err == nil && idx >= 0 && idx < len(shared) {
						val = shared[idx]
					}
				}
				cells = append(cells, val)
			case "row":
				line := strings.TrimSpace(strings.Join(cells, "\t"))
				if line != "" {
					rows = append(rows, line)
				}
				cells = nil
			}
		}
	}
	return rows, nil
}

func groupSheetRows(rows []string, sheetName string) []Block {
	const rowsPerBlock = 50
	var out []Block
	for start := 0; start < len(rows); start += rowsPerBlock {
		end := start + rowsPerBlock
		if end > len(rows) {
			end = len(rows)
		}
		text := strings.Join(rows[start:end], "\n")
		if strings.TrimSpace(text) == "" {
			continue
		}
		out = append(out, Block{Text: text, Location: Location{
			Kind: "sheet", Name: sheetName, Start: start + 1, End: end,
		}})
	}
	return out
}

// --- PowerPoint (.pptx) ---

func extractPptx(path string) ([]Block, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, &FileError{Reason: ReasonUnknownFormat, Detail: "pptx", Err: err}
	}
	defer func() { _ = zr.Close() }()

	var names []string
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "ppt/slides/slide") && strings.HasSuffix(f.Name, ".xml") {
			names = append(names, f.Name)
		}
	}
	sort.Slice(names, func(i, j int) bool { return naturalLess(names[i], names[j]) })
	if len(names) == 0 {
		return nil, &FileError{Reason: ReasonNoText, Err: ErrNoText}
	}

	var out []Block
	for i, name := range names {
		text, err := readSlideText(zr, name)
		if err != nil {
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		out = append(out, Block{Text: text, Location: Location{Kind: "slide", Start: i + 1}})
	}
	if len(out) == 0 {
		return nil, ErrNoText
	}
	return out, nil
}

func readSlideText(zr *zip.ReadCloser, name string) (string, error) { //nolint:gocyclo // streaming OOXML parser
	f := findZipFile(zr, name)
	if f == nil {
		return "", fmt.Errorf("slide %s not found", name)
	}
	rc, err := openZipXML(f)
	if err != nil {
		return "", err
	}
	defer func() { _ = rc.Close() }()

	dec := xml.NewDecoder(rc)
	var b strings.Builder
	inText := 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) || err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inText++
			case "br":
				b.WriteString("\n")
			case "p":
				if b.Len() > 0 {
					b.WriteString("\n")
				}
			}
		case xml.CharData:
			if inText > 0 {
				b.Write(t)
			}
		case xml.EndElement:
			if t.Name.Local == "t" && inText > 0 {
				inText--
			}
		}
	}
	return b.String(), nil
}

// naturalLess compares strings by their numeric runs, so slide2 sorts before
// slide10.
func naturalLess(a, b string) bool {
	ai, aNum := trailingNumber(a)
	bi, bNum := trailingNumber(b)
	if aNum && bNum && ai != bi {
		return ai < bi
	}
	return a < b
}

func trailingNumber(s string) (int, bool) {
	base := s
	if idx := strings.LastIndexByte(base, '/'); idx >= 0 {
		base = base[idx+1:]
	}
	if idx := strings.LastIndexByte(base, '.'); idx >= 0 {
		base = base[:idx]
	}
	end := len(base)
	start := end
	for start > 0 && base[start-1] >= '0' && base[start-1] <= '9' {
		start--
	}
	if start == end {
		return 0, false
	}
	n, err := strconv.Atoi(base[start:end])
	if err != nil {
		return 0, false
	}
	return n, true
}
