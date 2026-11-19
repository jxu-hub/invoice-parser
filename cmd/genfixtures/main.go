// Command genfixtures 生成测试用的合成表格。
//
// 为什么不用真实数据：客户表格里有订单号和客户名，不能进仓库。
// 所以这里按真实表格的"毛病"造 200 张表：标题行、组表头合并、
// 金额带 ¥ 和千分位、日期五六种写法、合计行、重复表头、脚注。
//
// 每张表都是真的 xlsx（zip + OOXML），不是改后缀的 CSV，
// 这样 xlsx 读取层和合并单元格逻辑都能被真实覆盖。
//
// 生成物：
//
//	testdata/raw/*.xlsx        200 张表
//	testdata/golden/*.csv      每张表的正确答案（由生成时的数据直接导出，不经过解析器）
//	testdata/labels.csv        文件 -> 失败类别，baseline 报告按它分组
//	testdata/manifest.json     跑批清单
package main

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	classClean  = "clean"
	classHeader = "header"
	classMerged = "merged"
	classMoney  = "money"
	classDate   = "date"
	classOther  = "other"

	// 200 张表的类别分布。总和必须等于 totalSheets。
	totalSheets = 200
	nClean      = 137
	nHeader     = 21
	nMerged     = 18
	nMoney      = 14
	nDate       = 7
	nOther      = 3
)

var (
	products = []string{"羽绒服", "保温杯", "蓝牙耳机", "毛线帽", "帆布包", "护手霜", "加绒手套", "折叠伞"}
	prices   = []float64{39.9, 68, 128, 259.5, 199, 1299, 15.8, 88, 460.5, 7200, 3200}
	notes    = []string{"", "", "", "补发", "加急", "赠品"}
)

type record struct {
	ID      string
	Date    time.Time
	Product string
	Price   float64
	Qty     int
	Amount  float64
	Note    string
}

type cell struct {
	text    string
	numeric bool
}

func txt(s string) cell { return cell{text: s} }
func num(v float64) cell {
	return cell{text: FormatFloat(v), numeric: true}
}
func numInt(v int) cell { return cell{text: strconv.Itoa(v), numeric: true} }

func main() {
	out := flag.String("out", "testdata", "输出目录")
	seed := flag.Int64("seed", 20240105, "随机种子，改动会改变 fixture 内容")
	flag.Parse()

	rnd := rand.New(rand.NewSource(*seed))

	if err := os.MkdirAll(filepath.Join(*out, "raw"), 0o755); err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(*out, "golden"), 0o755); err != nil {
		fatal(err)
	}

	labels := assignClasses(rnd)
	mf := sheetManifest{}

	for i, class := range labels {
		recs := makeRecords(rnd, 5+rnd.Intn(8))
		grid, merges, inline := buildSheet(recs, class, rnd)

		name := fmt.Sprintf("shop%02d-2024-%02d-%03d.xlsx", i%8+1, i%12+1, i+1)
		rawPath := filepath.Join("raw", name)
		if err := writeXLSX(filepath.Join(*out, rawPath), grid, merges, inline); err != nil {
			fatal(err)
		}

		goldenName := strings.TrimSuffix(name, ".xlsx") + ".csv"
		if err := writeGolden(filepath.Join(*out, "golden", goldenName), recs); err != nil {
			fatal(err)
		}

		mf.Sheets = append(mf.Sheets, sheetEntry{
			File:   rawPath,
			Golden: filepath.Join("golden", goldenName),
			Label:  class,
		})
	}

	if err := writeLabels(filepath.Join(*out, "labels.csv"), mf); err != nil {
		fatal(err)
	}
	if err := writeManifest(filepath.Join(*out, "manifest.json"), mf); err != nil {
		fatal(err)
	}
	fmt.Printf("生成 %d 张表: clean=%d header=%d merged=%d money=%d date=%d other=%d\n",
		len(mf.Sheets), nClean, nHeader, nMerged, nMoney, nDate, nOther)
}

// assignClasses 生成 200 个类别并打散，避免按类别排成连续的文件。
func assignClasses(rnd *rand.Rand) []string {
	out := make([]string, 0, totalSheets)
	add := func(class string, n int) {
		for i := 0; i < n; i++ {
			out = append(out, class)
		}
	}
	add(classClean, nClean)
	add(classHeader, nHeader)
	add(classMerged, nMerged)
	add(classMoney, nMoney)
	add(classDate, nDate)
	add(classOther, nOther)
	if len(out) != totalSheets {
		panic("类别数量之和必须等于 totalSheets")
	}
	rnd.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

func makeRecords(rnd *rand.Rand, n int) []record {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	recs := make([]record, n)
	for i := 0; i < n; i++ {
		price := prices[rnd.Intn(len(prices))]
		qty := 1 + rnd.Intn(6)
		amount := round2(price * float64(qty))
		note := notes[rnd.Intn(len(notes))]
		if rnd.Intn(10) == 0 { // 一成退款，金额为负
			amount = -amount
			note = "退款"
		}
		recs[i] = record{
			ID:      fmt.Sprintf("0000%04d", 1000+i*7+rnd.Intn(50)),
			Date:    base.AddDate(0, 0, i*3+rnd.Intn(3)),
			Product: products[rnd.Intn(len(products))],
			Price:   price,
			Qty:     qty,
			Amount:  amount,
			Note:    note,
		}
	}
	return recs
}

// buildSheet 按类别把数据渲染成网格。脏数据只改"写法"，不改事实，
// 所以每张表都能被可靠地还原成 golden 里的那几行。
func buildSheet(recs []record, class string, rnd *rand.Rand) (grid [][]cell, merges []string, inline bool) {
	inline = rnd.Intn(7) == 0 // 少数文件用 inlineStr，覆盖两种编码

	header := []cell{
		txt("订单编号"), txt("下单日期"), txt("商品名称"),
		txt("单价"), txt("数量"), txt("金额"), txt("备注"),
	}

	// 返回 0-based 行号，mergeRef 内部再 +1 转成 A1 引用。
	addRow := func(cells ...cell) int {
		grid = append(grid, cells)
		return len(grid) - 1
	}

	switch class {
	case classHeader:
		// 标题行 + 可能的空行/制表人行，表头在第 2~4 行
		switch rnd.Intn(3) {
		case 0:
			r := addRow(txt("深圳市恒昌贸易有限公司  销售明细"))
			merges = append(merges, mergeRef(r, 0, r, 6))
		case 1:
			addRow() // 空行
			r := addRow(txt("华南区域  2024 年销售明细"))
			merges = append(merges, mergeRef(r, 0, r, 6))
		default:
			r := addRow(txt("恒昌贸易  2024 年销售明细（内部）"))
			merges = append(merges, mergeRef(r, 0, r, 6))
			addRow(txt("制表人：张三    导出时间：2024-02-01 10:22"))
		}
		addRow(header...)

	case classMerged:
		// 两行表头：上面是合并的组表头，真正的列名在下面一行
		r := addRow(txt("订单信息"), txt(""), txt("商品"), txt("金额与数量"), txt(""), txt(""), txt("备注"))
		merges = append(merges,
			mergeRef(r, 0, r, 1),
			mergeRef(r, 3, r, 5),
		)
		addRow(header...)

	default:
		addRow(header...)
	}

	for _, rec := range recs {
		dateCell := txt(rec.Date.Format("2006-01-02"))
		priceCell := num(rec.Price)
		amountCell := num(rec.Amount)

		if class == classDate {
			dateCell = txt(renderDate(rec.Date, rnd.Intn(len(dateFormats))))
		}
		if class == classMoney {
			priceCell = txt(renderMoney(rec.Price, rnd.Intn(len(moneyStyles))))
			amountCell = txt(renderMoney(rec.Amount, rnd.Intn(len(moneyStyles))))
		}
		addRow(txt(rec.ID), dateCell, txt(rec.Product), priceCell, numInt(rec.Qty), amountCell, txt(rec.Note))
	}

	switch class {
	case classOther:
		switch rnd.Intn(3) {
		case 0:
			// 合计行
			var sum float64
			for _, rec := range recs {
				sum += rec.Amount
			}
			addRow(txt("合计"), txt(""), txt(""), txt(""), numInt(0), num(round2(sum)), txt(""))
		case 1:
			// 中途重复一遍表头
			addRow(header...)
		default:
			addRow(txt("注：本表由系统导出，金额单位：元"))
			addRow()
			addRow(txt("第 1 页 / 共 1 页"))
		}
	}

	return grid, merges, inline
}

var dateFormats = []string{
	"2006/1/2", "2006年1月2日", "20060102", "02-Jan-06",
	"2006.01.02", "2006-1-2", "2006年01月02日",
}

func renderDate(t time.Time, style int) string { return t.Format(dateFormats[style]) }

// moneyStyles 是金额的四种写法：¥ 前缀、元 后缀、括号负数、全角￥+元。
var moneyStyles = []string{"yen-prefix", "yuan-suffix", "paren-negative", "fullwidth-both"}

func renderMoney(v float64, style int) string {
	switch moneyStyles[style] {
	case "yen-prefix":
		return "¥" + thousands(v)
	case "yuan-suffix":
		return thousands(v) + "元"
	case "paren-negative":
		if v < 0 {
			return "(" + thousands(math.Abs(v)) + ")"
		}
		return thousands(v)
	default:
		return "￥" + thousands(v) + "元"
	}
}

func thousands(v float64) string {
	s := strconv.FormatFloat(math.Abs(v), 'f', 2, 64)
	intPart, frac, _ := strings.Cut(s, ".")
	var b strings.Builder
	for i, ch := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(ch)
	}
	out := b.String() + "." + frac
	if v < 0 {
		return "-" + out
	}
	return out
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func mergeRef(r1, c1, r2, c2 int) string {
	return colName(c1) + strconv.Itoa(r1+1) + ":" + colName(c2) + strconv.Itoa(r2+1)
}

func colName(c int) string {
	var s string
	for c >= 0 {
		s = string(rune('A'+c%26)) + s
		c = c/26 - 1
	}
	return s
}

// FormatFloat / FormatDate 必须和 parser 输出保持完全一致，
// 否则 golden 和实际结果会永远对不上。
func FormatFloat(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func writeGolden(path string, recs []record) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"order_id", "order_date", "product", "unit_price", "qty", "amount", "note"}); err != nil {
		return err
	}
	for _, r := range recs {
		row := []string{
			r.ID,
			r.Date.Format("2006-01-02"),
			r.Product,
			FormatFloat(r.Price),
			strconv.Itoa(r.Qty),
			FormatFloat(r.Amount),
			r.Note,
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return nil
}

type sheetEntry struct {
	File   string `json:"file"`
	Golden string `json:"golden"`
	Label  string `json:"label"`
}

type sheetManifest struct {
	Sheets []sheetEntry `json:"sheets"`
}

func writeLabels(path string, m sheetManifest) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"file", "label"}); err != nil {
		return err
	}
	for _, s := range m.Sheets {
		if err := w.Write([]string{s.File, s.Label}); err != nil {
			return err
		}
	}
	return nil
}

func writeManifest(path string, m sheetManifest) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// ---------------------------------------------------------------- xlsx 写入

func esc(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// writeXLSX 手写一个最小合法的 xlsx：zip + OOXML 各部分。
// 用 sharedStrings 是常规做法，inline 变体用来覆盖另一种编码。
func writeXLSX(path string, grid [][]cell, merges []string, inline bool) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	shared := []string{}
	index := map[string]int{}
	if !inline {
		for _, row := range grid {
			for _, c := range row {
				if c.numeric || c.text == "" {
					continue
				}
				if _, ok := index[c.text]; !ok {
					index[c.text] = len(shared)
					shared = append(shared, c.text)
				}
			}
		}
	}

	parts := map[string]string{
		"[Content_Types].xml":        contentTypes(len(shared) > 0),
		"_rels/.rels":                rootRels,
		"xl/workbook.xml":            workbookXML,
		"xl/_rels/workbook.xml.rels": workbookRels(len(shared) > 0),
		"xl/styles.xml":              stylesXML,
		"xl/worksheets/sheet1.xml":   sheetXML(grid, merges, inline, index),
	}
	if len(shared) > 0 {
		parts["xl/sharedStrings.xml"] = sharedStringsXML(shared)
	}

	for name, content := range parts {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte(content)); err != nil {
			return err
		}
	}
	return nil
}

func sheetXML(grid [][]cell, merges []string, inline bool, index map[string]int) string {
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for r, row := range grid {
		b.WriteString(fmt.Sprintf(`<row r="%d">`, r+1))
		for c, cl := range row {
			if cl.text == "" {
				continue
			}
			ref := colName(c) + strconv.Itoa(r+1)
			switch {
			case cl.numeric:
				b.WriteString(fmt.Sprintf(`<c r="%s"><v>%s</v></c>`, ref, esc(cl.text)))
			case inline:
				b.WriteString(fmt.Sprintf(`<c r="%s" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, esc(cl.text)))
			default:
				b.WriteString(fmt.Sprintf(`<c r="%s" t="s"><v>%d</v></c>`, ref, index[cl.text]))
			}
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData>`)
	if len(merges) > 0 {
		b.WriteString(fmt.Sprintf(`<mergeCells count="%d">`, len(merges)))
		for _, m := range merges {
			b.WriteString(fmt.Sprintf(`<mergeCell ref="%s"/>`, m))
		}
		b.WriteString(`</mergeCells>`)
	}
	b.WriteString(`</worksheet>`)
	return b.String()
}

func sharedStringsXML(items []string) string {
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString(fmt.Sprintf(`<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="%d" uniqueCount="%d">`, len(items), len(items)))
	for _, s := range items {
		b.WriteString(`<si><t xml:space="preserve">` + esc(s) + `</t></si>`)
	}
	b.WriteString(`</sst>`)
	return b.String()
}

func contentTypes(withShared bool) string {
	s := xml.Header + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`
	if withShared {
		s += `
<Override PartName="/xl/sharedStrings.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sharedStrings+xml"/>`
	}
	return s + "\n</Types>"
}

const rootRels = xml.Header + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`

const workbookXML = xml.Header + `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Sheet1" sheetId="1" r:id="rId1"/></sheets></workbook>`

func workbookRels(withShared bool) string {
	s := xml.Header + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`
	if withShared {
		s += `
<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/sharedStrings" Target="sharedStrings.xml"/>`
	}
	return s + "\n</Relationships>"
}

const stylesXML = xml.Header + `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="1"><font><sz val="11"/><name val="Calibri"/></font></fonts><fills count="1"><fill><patternFill patternType="none"/></fill></fills><borders count="1"><border/></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/></cellXfs></styleSheet>`

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "genfixtures: %v\n", err)
	os.Exit(1)
}
