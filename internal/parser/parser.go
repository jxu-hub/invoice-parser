package parser

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/jxu-hub/invoice-parser/internal/sheet"
)

// Row 是一行规范化之后的数据。
type Row struct {
	OrderID   string
	Date      string // 统一 YYYY-MM-DD
	Product   string
	UnitPrice float64
	Qty       int
	Amount    float64
	Note      string
}

// CSVHeader 是 golden 文件和 baseline 报告共用的列顺序。
var CSVHeader = []string{"order_id", "order_date", "product", "unit_price", "qty", "amount", "note"}

// Fields 按 CSVHeader 的顺序返回值，方便写 CSV。
func (r Row) Fields() []string {
	return []string{
		r.OrderID,
		r.Date,
		r.Product,
		FormatMoney(r.UnitPrice),
		fmt.Sprintf("%d", r.Qty),
		FormatMoney(r.Amount),
		r.Note,
	}
}

// IsolatedRow 是被隔离出来的行：这一行解析失败，但不拖垮整张表。
type IsolatedRow struct {
	Line   int // 在文件里的行号（1-based），方便人回去看原表
	Reason string
	Raw    []string
}

// Result 是一次解析的结果。
type Result struct {
	HeaderRow int
	Score     float64
	Schema    Schema
	Rows      []Row
	Isolated  []IsolatedRow
	Skipped   int // 被判定为非数据行（合计行、重复表头、脚注）而跳过的行数
}

// ErrNoHeader 表示在前 20 行里没找到可信的表头。
var ErrNoHeader = errors.New("parser: 未找到表头")

// SchemaError 表示表头找到了，但认不出必需的字段。
type SchemaError struct {
	HeaderRow int
	Missing   []Field
}

func (e *SchemaError) Error() string {
	names := make([]string, len(e.Missing))
	for i, f := range e.Missing {
		names[i] = string(f)
	}
	return fmt.Sprintf("parser: 第 %d 行是表头，但缺少字段 %s", e.HeaderRow+1, strings.Join(names, ","))
}

// Parse 把一张展平后的表解析成规范化行。
//
// 和旧实现的区别只有一条，但是根本性的：单行解析失败不再让整张表失败。
// 失败的行进 Isolated，带行号和原因，其余行照常输出 —— 200 张表里
// 平均有 6% 的行需要人工或模型复核，为了这 6% 把 94% 丢掉是不划算的。
func Parse(t *sheet.Table) (*Result, error) {
	headerRow, score, ok := DetectHeader(t)
	if !ok {
		return nil, ErrNoHeader
	}

	schema := buildSchema(t, headerRow, score)
	if missing := schema.Missing(); len(missing) > 0 {
		return nil, &SchemaError{HeaderRow: headerRow, Missing: missing}
	}

	res := &Result{HeaderRow: headerRow, Score: score, Schema: schema}

	cols := map[Field]Column{}
	for _, c := range schema.Columns {
		if c.Field != "" {
			cols[c.Field] = c
		}
	}

	headerNames := map[string]bool{}
	for _, v := range t.Rows[headerRow] {
		if v != "" {
			headerNames[NormalizeHeader(v)] = true
		}
	}

	for r := headerRow + 1; r < len(t.Rows); r++ {
		raw := t.Rows[r]
		if isEmptyRow(raw) {
			continue
		}
		if !isDataRow(raw, cols[FieldOrderID].Index, headerNames) {
			res.Skipped++
			continue
		}
		row, reason := parseRow(raw, cols)
		if reason != "" {
			res.Isolated = append(res.Isolated, IsolatedRow{Line: r + 1, Reason: reason, Raw: raw})
			continue
		}
		res.Rows = append(res.Rows, row)
	}
	return res, nil
}

// buildSchema 推断每一列的字段：先按列名匹配别名表，
// 匹配不上的列再按推断出的类型兜底。
func buildSchema(t *sheet.Table, headerRow int, score float64) Schema {
	schema := Schema{HeaderRow: headerRow, Score: score}
	width := t.Width()

	for c := 0; c < width; c++ {
		name := t.Rows[headerRow][c]
		kind, conf := InferKind(columnValues(t, headerRow, c))
		col := Column{Index: c, Name: name, Kind: kind, Confidence: conf}
		if f, ok := MapHeader(name); ok {
			col.Field, col.Matched = f, true
		}
		schema.Columns = append(schema.Columns, col)
	}

	// 类型兜底：只在"缺的字段"和"没认出来的列"数量正好对上时才认，
	// 对不上就不猜 —— 猜错的成本比缺一列高得多。
	fillByKind(&schema)
	return schema
}

func fillByKind(schema *Schema) {
	missing := schema.Missing()
	if len(missing) == 0 {
		return
	}
	unmatched := map[Kind][]int{}
	for i, c := range schema.Columns {
		if c.Field == "" && c.Kind != KindEmpty && c.Confidence >= 0.9 {
			unmatched[c.Kind] = append(unmatched[c.Kind], i)
		}
	}

	// 单值类型：一一对应就认
	single := map[Field]Kind{FieldDate: KindDate, FieldOrderID: KindID}
	for f, k := range single {
		if !hasField(missing, f) || len(unmatched[k]) != 1 {
			continue
		}
		i := unmatched[k][0]
		schema.Columns[i].Field = f
		unmatched[k] = nil
	}

	// 金额类字段：单价/数量/金额，只有数量正好等于缺的个数时才按顺序认
	var moneyFields []Field
	for _, f := range []Field{FieldUnitPrice, FieldQty, FieldAmount} {
		if hasField(missing, f) {
			moneyFields = append(moneyFields, f)
		}
	}
	if len(moneyFields) > 0 && len(unmatched[KindMoney]) == len(moneyFields) {
		for i, f := range moneyFields {
			schema.Columns[unmatched[KindMoney][i]].Field = f
		}
		unmatched[KindMoney] = nil
	}

	// 商品/备注是文本列，互为前后，只有在只剩一列时才敢认
	if textCols := unmatched[KindText]; len(textCols) == 1 {
		switch {
		case hasField(missing, FieldProduct) && !hasField(missing, FieldNote):
			schema.Columns[textCols[0]].Field = FieldProduct
		case hasField(missing, FieldNote) && !hasField(missing, FieldProduct):
			schema.Columns[textCols[0]].Field = FieldNote
		}
	}
}

func hasField(fs []Field, f Field) bool {
	for _, x := range fs {
		if x == f {
			return true
		}
	}
	return false
}

// noisePrefixes 是导出表格里固定出现、但不是数据的行。
// 把它们当数据行处理，结果就是整表失败或者多出几行脏数据。
var noisePrefixes = []string{
	"合计", "总计", "小计", "共计",
	"注", "说明", "数据来源", "制表", "导出时间", "页码",
	"第", // "第 1 页 / 共 1 页"
}

// isDataRow 判断一行是不是数据行。
// 合计行、脚注、中途重复出现的表头都不是 —— 它们的首列不是单号。
func isDataRow(raw []string, idCol int, headerNames map[string]bool) bool {
	if isNoiseRow(raw) {
		return false
	}
	if idCol < 0 || idCol >= len(raw) {
		return false
	}
	id := raw[idCol]
	if headerNames[NormalizeHeader(id)] {
		return false
	}
	if IsID(id) {
		return true
	}
	// 有些表把单号列放在别的列，首列是空的；这种行如果只有一个非空格子，
	// 基本就是脚注，丢掉。
	return !onlyOneCell(raw)
}

// isNoiseRow 判断这一行是不是合计/脚注/页码这类固定噪音。
func isNoiseRow(raw []string) bool {
	for _, v := range raw {
		if v == "" {
			continue
		}
		for _, p := range noisePrefixes {
			if strings.HasPrefix(v, p) {
				return true
			}
		}
		return false // 只看第一个非空格子，避免把备注里的"仅供参考"误杀
	}
	return false
}

func onlyOneCell(raw []string) bool {
	n := 0
	for _, v := range raw {
		if v != "" {
			n++
		}
	}
	return n <= 1
}

func isEmptyRow(raw []string) bool {
	for _, v := range raw {
		if v != "" {
			return false
		}
	}
	return true
}

func parseRow(raw []string, cols map[Field]Column) (Row, string) {
	var row Row
	get := func(f Field) string {
		c, ok := cols[f]
		if !ok || c.Index >= len(raw) {
			return ""
		}
		return strings.TrimSpace(raw[c.Index])
	}

	row.OrderID = get(FieldOrderID)
	row.Product = get(FieldProduct)
	row.Note = get(FieldNote)

	d, err := ParseDate(get(FieldDate))
	if err != nil {
		return Row{}, fmt.Sprintf("日期解析失败: %v", err)
	}
	row.Date = FormatDate(d)

	price, err := ParseMoney(get(FieldUnitPrice))
	if err != nil {
		return Row{}, fmt.Sprintf("单价解析失败: %v", err)
	}
	row.UnitPrice = price

	amount, err := ParseMoney(get(FieldAmount))
	if err != nil {
		return Row{}, fmt.Sprintf("金额解析失败: %v", err)
	}
	row.Amount = amount

	qty, err := ParseMoney(get(FieldQty))
	if err != nil {
		return Row{}, fmt.Sprintf("数量解析失败: %v", err)
	}
	row.Qty = int(math.Round(qty))

	if row.OrderID == "" {
		return Row{}, "缺少订单编号"
	}
	return row, ""
}
