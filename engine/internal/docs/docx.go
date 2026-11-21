package docs

// A minimal Office Open XML (Word .docx) writer: headings, paragraphs with
// bold and italic runs, bullet and numbered lists, simple tables, and one
// header and footer. No external dependencies. The output is deterministic:
// the same document gives the same bytes, so a content hash identifies it.

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

// Run is a piece of text with one format.
type Run struct {
	Text   string
	Bold   bool
	Italic bool
	Code   bool
}

// Block is one block of a document: *Heading, *Paragraph, *ListItem or *Table.
type Block interface{ block() }

// Heading is a heading of level 1 to 3.
type Heading struct {
	Level int
	Runs  []Run
}

// Paragraph is a paragraph of text.
type Paragraph struct{ Runs []Run }

// ListItem is one item of a bullet (Ordered false) or numbered list. List
// is the list's number in the document: a numbered list restarts at 1.
type ListItem struct {
	Ordered bool
	Level   int // 0 or 1
	List    int
	Runs    []Run
}

// Table is a table whose first row is the header.
type Table struct {
	Header []string
	Rows   [][]string
}

func (*Heading) block()   {}
func (*Paragraph) block() {}
func (*ListItem) block()  {}
func (*Table) block()     {}

// Document is what the writer turns into a .docx.
type Document struct {
	Title  string // document properties
	Header string // the page header line
	Footer string // the page footer line
	Blocks []Block
}

// zipTime is the fixed modification time of every part, so the same
// document always gives the same bytes.
var zipTime = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

// TextParts are the parts whose content the box hashes to recognise its
// own file. SharePoint may write column values into a document's property
// parts (docProps/, customXml/) on upload; it does not change these.
var TextParts = []string{"word/document.xml", "word/footer1.xml", "word/header1.xml"}

// DOCX returns the document as a .docx file.
func (d *Document) DOCX() ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", contentTypes},
		{"_rels/.rels", rootRels},
		{"docProps/core.xml", coreXML(d.Title)},
		{"word/_rels/document.xml.rels", documentRels},
		{"word/styles.xml", stylesXML},
		{"word/numbering.xml", numberingXML(d.Blocks)},
		{"word/header1.xml", headerFooter("hdr", "Header", d.Header)},
		{"word/footer1.xml", headerFooter("ftr", "Footer", d.Footer)},
		{"word/document.xml", documentXML(d.Blocks)},
	}
	for _, p := range parts {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: p.name, Method: zip.Deflate, Modified: zipTime})
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(w, p.body); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// TextHash is the hex SHA-256 of a .docx file's text parts (TextParts, in
// that order, each as name, NUL, content, NUL; a missing part is empty).
// The sharepoint.policies step computes the same hash in PowerShell to tell
// whether a file in SharePoint is still the one the box wrote.
func TextHash(docx []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(docx), int64(len(docx)))
	if err != nil {
		return "", err
	}
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	h := sha256.New()
	for _, name := range TextParts {
		h.Write([]byte(name))
		h.Write([]byte{0})
		if f := files[name]; f != nil {
			rc, err := f.Open()
			if err != nil {
				return "", err
			}
			_, err = io.Copy(h, rc)
			rc.Close()
			if err != nil {
				return "", err
			}
		}
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

const xmlHead = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"
const nsW = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`

const contentTypes = xmlHead + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
	`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
	`<Default Extension="xml" ContentType="application/xml"/>` +
	`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
	`<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>` +
	`<Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/>` +
	`<Override PartName="/word/header1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml"/>` +
	`<Override PartName="/word/footer1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.footer+xml"/>` +
	`<Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>` +
	`</Types>`

const rootRels = xmlHead + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
	`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>` +
	`</Relationships>`

const documentRels = xmlHead + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>` +
	`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>` +
	`<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header1.xml"/>` +
	`<Relationship Id="rId4" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer1.xml"/>` +
	`</Relationships>`

const stylesXML = xmlHead + `<w:styles ` + nsW + `>` +
	`<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsia="Calibri" w:cs="Calibri"/><w:sz w:val="22"/><w:szCs w:val="22"/><w:lang w:val="en-GB"/></w:rPr></w:rPrDefault>` +
	`<w:pPrDefault><w:pPr><w:spacing w:after="120" w:line="264" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults>` +
	`<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:spacing w:before="240" w:after="120"/><w:outlineLvl w:val="0"/></w:pPr><w:rPr><w:b/><w:sz w:val="32"/><w:szCs w:val="32"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:spacing w:before="200" w:after="80"/><w:outlineLvl w:val="1"/></w:pPr><w:rPr><w:b/><w:sz w:val="26"/><w:szCs w:val="26"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading3"><w:name w:val="heading 3"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:spacing w:before="160" w:after="60"/><w:outlineLvl w:val="2"/></w:pPr><w:rPr><w:b/><w:sz w:val="23"/><w:szCs w:val="23"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="ListParagraph"><w:name w:val="List Paragraph"/><w:basedOn w:val="Normal"/><w:qFormat/><w:pPr><w:spacing w:after="60"/><w:ind w:left="720"/><w:contextualSpacing/></w:pPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Header"><w:name w:val="header"/><w:basedOn w:val="Normal"/><w:rPr><w:sz w:val="18"/><w:szCs w:val="18"/><w:color w:val="555555"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Footer"><w:name w:val="footer"/><w:basedOn w:val="Normal"/><w:rPr><w:sz w:val="18"/><w:szCs w:val="18"/><w:color w:val="555555"/></w:rPr></w:style>` +
	`<w:style w:type="table" w:default="1" w:styleId="TableNormal"><w:name w:val="Normal Table"/><w:tblPr><w:tblInd w:w="0" w:type="dxa"/><w:tblCellMar><w:top w:w="0" w:type="dxa"/><w:left w:w="108" w:type="dxa"/><w:bottom w:w="0" w:type="dxa"/><w:right w:w="108" w:type="dxa"/></w:tblCellMar></w:tblPr></w:style>` +
	`<w:style w:type="table" w:styleId="TableGrid"><w:name w:val="Table Grid"/><w:basedOn w:val="TableNormal"/><w:pPr><w:spacing w:after="0" w:line="240" w:lineRule="auto"/></w:pPr><w:tblPr><w:tblBorders>` +
	`<w:top w:val="single" w:sz="4" w:space="0" w:color="999999"/><w:left w:val="single" w:sz="4" w:space="0" w:color="999999"/><w:bottom w:val="single" w:sz="4" w:space="0" w:color="999999"/><w:right w:val="single" w:sz="4" w:space="0" w:color="999999"/><w:insideH w:val="single" w:sz="4" w:space="0" w:color="999999"/><w:insideV w:val="single" w:sz="4" w:space="0" w:color="999999"/>` +
	`</w:tblBorders></w:tblPr></w:style>` +
	`</w:styles>`

func coreXML(title string) string {
	return xmlHead + `<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">` +
		`<dc:title>` + esc(title) + `</dc:title><dc:creator>Company in a Box</dc:creator></cp:coreProperties>`
}

// numberingXML defines bullets (numId 1) and one numbering per numbered
// list (numId 2, 3, ...), each restarting at 1.
func numberingXML(blocks []Block) string {
	lists := 0
	for _, b := range blocks {
		if li, ok := b.(*ListItem); ok && li.Ordered && li.List > lists {
			lists = li.List
		}
	}
	var s strings.Builder
	s.WriteString(xmlHead + `<w:numbering ` + nsW + `>`)
	s.WriteString(`<w:abstractNum w:abstractNumId="0"><w:multiLevelType w:val="hybridMultilevel"/>` +
		`<w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="bullet"/><w:lvlText w:val="•"/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="720" w:hanging="360"/></w:pPr></w:lvl>` +
		`<w:lvl w:ilvl="1"><w:start w:val="1"/><w:numFmt w:val="bullet"/><w:lvlText w:val="–"/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="1440" w:hanging="360"/></w:pPr></w:lvl></w:abstractNum>`)
	s.WriteString(`<w:abstractNum w:abstractNumId="1"><w:multiLevelType w:val="hybridMultilevel"/>` +
		`<w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%1."/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="720" w:hanging="360"/></w:pPr></w:lvl>` +
		`<w:lvl w:ilvl="1"><w:start w:val="1"/><w:numFmt w:val="lowerLetter"/><w:lvlText w:val="%2."/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="1440" w:hanging="360"/></w:pPr></w:lvl></w:abstractNum>`)
	s.WriteString(`<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>`)
	for i := 1; i <= lists; i++ {
		fmt.Fprintf(&s, `<w:num w:numId="%d"><w:abstractNumId w:val="1"/><w:lvlOverride w:ilvl="0"><w:startOverride w:val="1"/></w:lvlOverride></w:num>`, i+1)
	}
	s.WriteString(`</w:numbering>`)
	return s.String()
}

func headerFooter(tag, style, text string) string {
	return xmlHead + `<w:` + tag + ` ` + nsW + `>` + para(style, "", []Run{{Text: text}}) + `</w:` + tag + `>`
}

// textWidth is the width between the margins of an A4 page with 2.54 cm
// margins, in twentieths of a point.
const textWidth = 9026

func documentXML(blocks []Block) string {
	var s strings.Builder
	s.WriteString(xmlHead + `<w:document ` + nsW + `><w:body>`)
	last := ""
	for _, b := range blocks {
		switch x := b.(type) {
		case *Heading:
			lvl := min(max(x.Level, 1), 3)
			s.WriteString(para(fmt.Sprintf("Heading%d", lvl), "", x.Runs))
			last = "p"
		case *Paragraph:
			s.WriteString(para("", "", x.Runs))
			last = "p"
		case *ListItem:
			num := 1
			if x.Ordered {
				num = x.List + 1
			}
			s.WriteString(para("ListParagraph", fmt.Sprintf(`<w:numPr><w:ilvl w:val="%d"/><w:numId w:val="%d"/></w:numPr>`, min(max(x.Level, 0), 1), num), x.Runs))
			last = "p"
		case *Table:
			s.WriteString(table(x))
			last = "tbl"
		}
	}
	if last != "p" {
		s.WriteString(`<w:p/>`) // Word expects the body to end with a paragraph
	}
	s.WriteString(`<w:sectPr><w:headerReference w:type="default" r:id="rId3"/><w:footerReference w:type="default" r:id="rId4"/>` +
		`<w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="708" w:footer="708" w:gutter="0"/></w:sectPr>`)
	s.WriteString(`</w:body></w:document>`)
	return s.String()
}

func para(style, extra string, runs []Run) string {
	var s strings.Builder
	s.WriteString(`<w:p>`)
	if style != "" || extra != "" {
		s.WriteString(`<w:pPr>`)
		if style != "" {
			s.WriteString(`<w:pStyle w:val="` + style + `"/>`)
		}
		s.WriteString(extra)
		s.WriteString(`</w:pPr>`)
	}
	for _, r := range runs {
		s.WriteString(run(r))
	}
	s.WriteString(`</w:p>`)
	return s.String()
}

func run(r Run) string {
	if r.Text == "" {
		return ""
	}
	var props string
	if r.Bold {
		props += `<w:b/>`
	}
	if r.Italic {
		props += `<w:i/>`
	}
	if r.Code {
		props += `<w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:cs="Consolas"/>`
	}
	s := `<w:r>`
	if props != "" {
		s += `<w:rPr>` + props + `</w:rPr>`
	}
	return s + `<w:t xml:space="preserve">` + esc(r.Text) + `</w:t></w:r>`
}

func table(t *Table) string {
	cols := len(t.Header)
	for _, r := range t.Rows {
		cols = max(cols, len(r))
	}
	if cols == 0 {
		return ""
	}
	// Columns share the width by how much text they hold (header and
	// longest cell, within limits), so short codes stay narrow.
	weight := make([]int, cols)
	total := 0
	for i := 0; i < cols; i++ {
		m := 0
		if i < len(t.Header) {
			m = len(t.Header[i])
		}
		for _, r := range t.Rows {
			if i < len(r) {
				m = max(m, len(r[i]))
			}
		}
		weight[i] = min(max(m, 8), 60)
		total += weight[i]
	}
	widths := make([]int, cols)
	for i := range widths {
		widths[i] = textWidth * weight[i] / total
	}
	var s strings.Builder
	s.WriteString(`<w:tbl><w:tblPr><w:tblStyle w:val="TableGrid"/><w:tblW w:w="5000" w:type="pct"/><w:tblLook w:val="04A0" w:firstRow="1" w:lastRow="0" w:firstColumn="0" w:lastColumn="0" w:noHBand="0" w:noVBand="1"/></w:tblPr><w:tblGrid>`)
	for i := 0; i < cols; i++ {
		fmt.Fprintf(&s, `<w:gridCol w:w="%d"/>`, widths[i])
	}
	s.WriteString(`</w:tblGrid>`)
	row := func(cells []string, header bool) {
		s.WriteString(`<w:tr>`)
		if header {
			s.WriteString(`<w:trPr><w:tblHeader/></w:trPr>`)
		}
		for i := 0; i < cols; i++ {
			c := ""
			if i < len(cells) {
				c = cells[i]
			}
			runs := Inline(c)
			if header {
				for j := range runs {
					runs[j].Bold = true
				}
			}
			fmt.Fprintf(&s, `<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/></w:tcPr>%s</w:tc>`, widths[i], para("", "", runs))
		}
		s.WriteString(`</w:tr>`)
	}
	if len(t.Header) > 0 {
		row(t.Header, true)
	}
	for _, r := range t.Rows {
		row(r, false)
	}
	s.WriteString(`</w:tbl>`)
	return s.String()
}

// invalidXML matches characters XML 1.0 does not allow.
var invalidXML = regexp.MustCompile("[\x00-\x08\x0B\x0C\x0E-\x1F￾￿]")

func esc(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(invalidXML.ReplaceAllString(s, "")))
	return b.String()
}

// ---------- Markdown ----------

var (
	headingRe = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)
	bulletRe  = regexp.MustCompile(`^(\s*)[-*+]\s+(.*)$`)
	orderedRe = regexp.MustCompile(`^(\s*)\d+[.)]\s+(.*)$`)
	ruleRe    = regexp.MustCompile(`^\s*([-*_]\s*){3,}$`)
	sepCellRe = regexp.MustCompile(`^\s*:?-{1,}:?\s*$`)
)

// ParseMarkdown turns the Markdown the templates use into blocks: ATX
// headings, paragraphs, bullet and numbered lists (two levels), pipe
// tables, and **bold**, *italic*, `code` and [links](url) inline.
// Horizontal rules are dropped; a block quote is a paragraph.
func ParseMarkdown(md string) []Block {
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	var out []Block
	var para []string
	list, inOrdered := 0, false
	flush := func() {
		if len(para) > 0 {
			out = append(out, &Paragraph{Runs: Inline(strings.Join(para, " "))})
			para = nil
		}
	}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trim := strings.TrimSpace(line)
		isOrdered := false
		switch {
		case trim == "":
			flush()
		case ruleRe.MatchString(trim):
			flush()
		case headingRe.MatchString(trim):
			flush()
			m := headingRe.FindStringSubmatch(trim)
			out = append(out, &Heading{Level: len(m[1]), Runs: Inline(m[2])})
		case strings.HasPrefix(trim, "|"):
			flush()
			var rows [][]string
			for ; i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|"); i++ {
				rows = append(rows, splitRow(strings.TrimSpace(lines[i])))
			}
			i--
			t := &Table{}
			if len(rows) > 0 {
				t.Header = rows[0]
				rows = rows[1:]
			}
			if len(rows) > 0 && isSeparator(rows[0]) {
				rows = rows[1:]
			}
			t.Rows = rows
			out = append(out, t)
		case orderedRe.MatchString(line):
			flush()
			m := orderedRe.FindStringSubmatch(line)
			isOrdered = true
			if !inOrdered {
				list++
			}
			out = append(out, &ListItem{Ordered: true, Level: indentLevel(m[1]), List: list, Runs: Inline(m[2])})
		case bulletRe.MatchString(line):
			flush()
			m := bulletRe.FindStringSubmatch(line)
			out = append(out, &ListItem{Level: indentLevel(m[1]), Runs: Inline(m[2])})
		default:
			if len(para) == 0 && len(out) > 0 && strings.HasPrefix(line, "  ") {
				// A continuation line of the list item above.
				if li, ok := out[len(out)-1].(*ListItem); ok {
					li.Runs = append(li.Runs, Inline(" "+trim)...)
					isOrdered = li.Ordered
					break
				}
			}
			para = append(para, strings.TrimPrefix(trim, "> "))
		}
		if trim != "" {
			inOrdered = isOrdered
		}
	}
	flush()
	return out
}

func indentLevel(ws string) int {
	if len(strings.ReplaceAll(ws, "\t", "    ")) >= 2 {
		return 1
	}
	return 0
}

// splitRow splits a pipe table row on unescaped pipes; "\|" is a pipe in a cell.
func splitRow(line string) []string {
	line = strings.TrimSuffix(strings.TrimPrefix(line, "|"), "|")
	if strings.HasSuffix(line, `\`) {
		line += "|" // the trailing pipe was escaped
	}
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		switch {
		case line[i] == '\\' && i+1 < len(line) && line[i+1] == '|':
			cur.WriteByte('|')
			i++
		case line[i] == '|':
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(line[i])
		}
	}
	return append(cells, strings.TrimSpace(cur.String()))
}

func isSeparator(cells []string) bool {
	for _, c := range cells {
		if !sepCellRe.MatchString(c) {
			return false
		}
	}
	return len(cells) > 0
}

var linkRe = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)

// Inline splits text into runs on **bold**, __bold__, *italic*, _italic_
// and `code`; a link [text](url) reads "text (url)".
func Inline(s string) []Run {
	s = linkRe.ReplaceAllStringFunc(s, func(m string) string {
		p := linkRe.FindStringSubmatch(m)
		if p[1] == p[2] {
			return p[1]
		}
		return p[1] + " (" + p[2] + ")"
	})
	var out []Run
	var cur strings.Builder
	bold, italic := false, false
	emit := func(code bool) {
		if cur.Len() > 0 {
			out = append(out, Run{Text: cur.String(), Bold: bold, Italic: italic, Code: code})
			cur.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s) && strings.IndexByte("*_`\\|[]#", s[i+1]) >= 0:
			cur.WriteByte(s[i+1])
			i++
		case (c == '*' || c == '_') && i+1 < len(s) && s[i+1] == c:
			emit(false)
			bold = !bold
			i++
		case c == '*' || (c == '_' && (i == 0 || !isWord(s[i-1]) || i+1 >= len(s) || !isWord(s[i+1]))):
			emit(false)
			italic = !italic
		case c == '`':
			if j := strings.IndexByte(s[i+1:], '`'); j >= 0 {
				emit(false)
				cur.WriteString(s[i+1 : i+1+j])
				emit(true)
				i += j + 1
				continue
			}
			cur.WriteByte(c)
		default:
			cur.WriteByte(c)
		}
	}
	emit(false)
	return out
}

func isWord(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// PlainText is the blocks' text without formatting, for previews and tests.
func PlainText(blocks []Block) string {
	runs := func(rs []Run) string {
		var b strings.Builder
		for _, r := range rs {
			b.WriteString(r.Text)
		}
		return b.String()
	}
	var b strings.Builder
	for _, bl := range blocks {
		switch x := bl.(type) {
		case *Heading:
			b.WriteString(runs(x.Runs) + "\n")
		case *Paragraph:
			b.WriteString(runs(x.Runs) + "\n")
		case *ListItem:
			b.WriteString("- " + runs(x.Runs) + "\n")
		case *Table:
			b.WriteString(strings.Join(x.Header, " | ") + "\n")
			for _, r := range x.Rows {
				b.WriteString(strings.Join(r, " | ") + "\n")
			}
		}
	}
	return b.String()
}
