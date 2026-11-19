// Package xlsx 是一个最小可用的 xlsx 读取器。
//
// 只依赖标准库：xlsx 本质是一个 zip，里面是若干 XML。
// 这里只做三件事，但都是解析脏表格必须的：
//
//  1. 按 workbook -> rels -> worksheet 的顺序找到第一张工作表（不硬编码 sheet1.xml）
//  2. 支持 sharedStrings / inlineStr / 数字三种单元格
//  3. 读回合并单元格区域，交给上层决定怎么展平
//
// 不支持公式求值、样式、多工作表选择：这个项目不需要，也不需要为此引入第三方依赖。
package xlsx

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

// Range 是一个合并单元格区域，行列均为 0-based、闭区间。
type Range struct {
	R1, C1, R2, C2 int
}

// Sheet 是读出来的工作表。
// Rows 已经补齐成矩形：短行会用空字符串填充，方便按列访问；
// 合并区域的左上角保留值，其余格子在 Rows 里仍是空串，展平交给 sheet 包。
type Sheet struct {
	Name   string
	Rows   [][]string
	Merges []Range
}

var errNoSheet = fmt.Errorf("xlsx: workbook 里没有工作表")

// Open 读取 xlsx 文件里的第一张工作表。
func Open(file string) (*Sheet, error) {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return nil, fmt.Errorf("xlsx: open %s: %w", file, err)
	}
	defer zr.Close()

	files := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		files[f.Name] = f
	}

	name, sheetPath, err := locateFirstSheet(zr, files)
	if err != nil {
		return nil, err
	}

	shared, err := readSharedStrings(files)
	if err != nil {
		return nil, err
	}

	f, ok := files[sheetPath]
	if !ok {
		return nil, fmt.Errorf("xlsx: 缺少工作表文件 %q", sheetPath)
	}
	rows, merges, err := readSheet(f, shared)
	if err != nil {
		return nil, fmt.Errorf("xlsx: 解析 %s: %w", sheetPath, err)
	}

	return &Sheet{Name: name, Rows: rows, Merges: merges}, nil
}

type workbookXML struct {
	Sheets []struct {
		Name string `xml:"name,attr"`
		RID  string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
	} `xml:"sheets>sheet"`
}

type relsXML struct {
	Relationships []struct {
		ID     string `xml:"Id,attr"`
		Target string `xml:"Target,attr"`
	} `xml:"Relationship"`
}

// locateFirstSheet 顺着 workbook + rels 找到第一张工作表的真实路径。
// 找不到关系表时退回 sheet1.xml —— 有些导出工具不写 rels。
func locateFirstSheet(zr *zip.ReadCloser, files map[string]*zip.File) (name, p string, err error) {
	def := "xl/worksheets/sheet1.xml"
	f, ok := files["xl/workbook.xml"]
	if !ok {
		if _, ok := files[def]; ok {
			return "Sheet1", def, nil
		}
		return "", "", errNoSheet
	}

	var wb workbookXML
	if e := decodeFile(f, &wb); e != nil {
		return "Sheet1", def, nil
	}
	if len(wb.Sheets) == 0 {
		return "", "", errNoSheet
	}
	name = wb.Sheets[0].Name
	if name == "" {
		name = "Sheet1"
	}
	rid := wb.Sheets[0].RID

	rf, ok := files["xl/_rels/workbook.xml.rels"]
	if !ok || rid == "" {
		return name, def, nil
	}
	var rels relsXML
	if e := decodeFile(rf, &rels); e != nil {
		return name, def, nil
	}
	for _, r := range rels.Relationships {
		if r.ID != rid {
			continue
		}
		target := strings.TrimPrefix(r.Target, "/")
		if !strings.HasPrefix(target, "xl/") {
			target = path.Join("xl", target)
		}
		if _, ok := files[target]; ok {
			return name, target, nil
		}
	}
	return name, def, nil
}

func decodeFile(f *zip.File, v any) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	return xml.NewDecoder(rc).Decode(v)
}

type xString struct {
	T    string `xml:"t"`
	Runs []xRun `xml:"r"`
}

type xRun struct {
	T string `xml:"t"`
}

func (s xString) String() string {
	if len(s.Runs) == 0 {
		return s.T
	}
	var b strings.Builder
	for _, r := range s.Runs {
		b.WriteString(r.T)
	}
	return b.String()
}

type sharedStringsXML struct {
	SI []xString `xml:"si"`
}

func readSharedStrings(files map[string]*zip.File) ([]string, error) {
	f, ok := files["xl/sharedStrings.xml"]
	if !ok {
		return nil, nil
	}
	var sst sharedStringsXML
	if err := decodeFile(f, &sst); err != nil {
		return nil, fmt.Errorf("xlsx: 解析 sharedStrings: %w", err)
	}
	out := make([]string, len(sst.SI))
	for i, s := range sst.SI {
		out[i] = s.String()
	}
	return out, nil
}

type sheetXML struct {
	Rows       []xRow   `xml:"sheetData>row"`
	MergeCells []xMerge `xml:"mergeCells>mergeCell"`
}

type xRow struct {
	R     int     `xml:"r,attr"`
	Cells []xCell `xml:"c"`
}

type xCell struct {
	R  string  `xml:"r,attr"`
	T  string  `xml:"t,attr"`
	V  string  `xml:"v"`
	IS xString `xml:"is"`
}

type xMerge struct {
	Ref string `xml:"ref,attr"`
}

func readSheet(f *zip.File, shared []string) ([][]string, []Range, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, nil, err
	}
	defer rc.Close()

	var s sheetXML
	if err := xml.NewDecoder(rc).Decode(&s); err != nil && err != io.EOF {
		return nil, nil, err
	}

	// 先定位最大行列，避免一边扩容一边填。
	maxRow, maxCol := 0, 0
	for _, r := range s.Rows {
		rn := r.R
		if rn == 0 {
			rn = maxRow + 1
		}
		if rn > maxRow {
			maxRow = rn
		}
		for _, c := range r.Cells {
			_, col, err := splitRef(c.R)
			if err != nil {
				continue
			}
			if col > maxCol {
				maxCol = col
			}
		}
	}

	grid := make([][]string, maxRow)
	for i := range grid {
		grid[i] = make([]string, maxCol+1)
	}

	prevRow := 0
	for _, r := range s.Rows {
		rn := r.R
		if rn == 0 {
			rn = prevRow + 1
		}
		prevRow = rn
		if rn > len(grid) {
			continue
		}
		row := grid[rn-1]
		prevCol := -1
		for _, c := range r.Cells {
			col := prevCol + 1
			if c.R != "" {
				_, parsed, err := splitRef(c.R)
				if err == nil {
					col = parsed
				}
			}
			prevCol = col
			if col >= len(row) {
				continue
			}
			row[col] = c.value(shared)
		}
	}

	merges := make([]Range, 0, len(s.MergeCells))
	for _, m := range s.MergeCells {
		if r, err := parseRange(m.Ref); err == nil {
			merges = append(merges, r)
		}
	}
	return grid, merges, nil
}

func (c xCell) value(shared []string) string {
	switch c.T {
	case "s":
		i, err := strconv.Atoi(strings.TrimSpace(c.V))
		if err != nil || i < 0 || i >= len(shared) {
			return ""
		}
		return shared[i]
	case "inlineStr":
		return c.IS.String()
	case "str", "e", "b":
		return c.V
	case "n":
		return expandNumber(c.V)
	default:
		// 没有 t 属性时是数字；共享字符串以外的文本也走这里。
		if strings.TrimSpace(c.V) == "" {
			return c.IS.String()
		}
		return expandNumber(c.V)
	}
}

// expandNumber 把 1.23456789E7 这类科学计数法还原成普通写法，
// 避免下游把账号/单号误判成浮点。
func expandNumber(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if !strings.ContainsAny(s, "eE") {
		return s
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// splitRef 解析 "AB12" 这样的单元格引用，返回 0-based 行、列。
func splitRef(ref string) (row, col int, err error) {
	ref = strings.TrimSpace(ref)
	i := 0
	for i < len(ref) && isLetter(ref[i]) {
		i++
	}
	if i == 0 || i == len(ref) {
		return 0, 0, fmt.Errorf("bad ref %q", ref)
	}
	col = 0
	for _, ch := range strings.ToUpper(ref[:i]) {
		col = col*26 + int(ch-'A'+1)
	}
	col--
	r, err := strconv.Atoi(ref[i:])
	if err != nil || r <= 0 {
		return 0, 0, fmt.Errorf("bad ref %q", ref)
	}
	return r - 1, col, nil
}

func parseRange(ref string) (Range, error) {
	parts := strings.Split(ref, ":")
	if len(parts) != 2 {
		return Range{}, fmt.Errorf("bad merge %q", ref)
	}
	r1, c1, err := splitRef(parts[0])
	if err != nil {
		return Range{}, err
	}
	r2, c2, err := splitRef(parts[1])
	if err != nil {
		return Range{}, err
	}
	return Range{R1: r1, C1: c1, R2: r2, C2: c2}, nil
}

func isLetter(b byte) bool { return b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' }
