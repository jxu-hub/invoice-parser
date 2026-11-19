// Package parser 把表格整理成规范化的订单行。
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

// Result 是一次解析的结果。
type Result struct {
	HeaderRow int
	Score     float64
	Schema    Schema
	Rows      []Row
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

// Parse 把一张表解析成规范化行。
//
// 和旧实现的区别：表头不再假设在第 1 行，而是打分选出来的。
// 值解析仍然是严格的——一行解析不了，整张表还是失败，这一步留到后面再改。
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

	for r := headerRow + 1; r < len(t.Rows); r++ {
		raw := t.Rows[r]
		if isEmptyRow(raw) {
			continue
		}
		row, err := parseRow(raw, cols)
		if err != nil {
			return nil, fmt.Errorf("第 %d 行: %w", r+1, err)
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

func isEmptyRow(raw []string) bool {
	for _, v := range raw {
		if v != "" {
			return false
		}
	}
	return true
}

func parseRow(raw []string, cols map[Field]Column) (Row, error) {
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
		return Row{}, err
	}
	row.Date = FormatDate(d)

	if row.UnitPrice, err = ParseMoney(get(FieldUnitPrice)); err != nil {
		return Row{}, err
	}
	if row.Amount, err = ParseMoney(get(FieldAmount)); err != nil {
		return Row{}, err
	}
	qty, err := ParseMoney(get(FieldQty))
	if err != nil {
		return Row{}, err
	}
	row.Qty = int(math.Round(qty))
	return row, nil
}
