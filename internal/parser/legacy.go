package parser

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// legacyHeader 是第一版实现的列名映射：只认完全一致的写法，没有别名。
//
// 这一版就是 baseline 里的 137/200 —— 留着它是为了证明后续每一版改动
// 到底把成功率抬高了多少，同时也是回归测试的一部分：
// 谁要是把 ParseLegacy 的通过数改动了，gate 会立刻红。
var legacyHeader = map[string]Field{
	"订单编号": FieldOrderID,
	"下单日期": FieldDate,
	"商品名称": FieldProduct,
	"单价":   FieldUnitPrice,
	"数量":   FieldQty,
	"金额":   FieldAmount,
	"备注":   FieldNote,
}

// ParseLegacy 是第一版解析器：假设第一行是表头、列名完全规范、
// 每个值都能严格解析，任何一处不满足，整张表失败。
//
// 它接收的是未展平的原始网格——合并单元格在这一版里会直接读成空格，
// 这正是它在合并表头上全错的原因。
func ParseLegacy(rows [][]string) ([]Row, error) {
	if len(rows) < 2 {
		return nil, fmt.Errorf("legacy: 表格为空")
	}

	cols := map[Field]int{}
	for i, h := range rows[0] {
		if f, ok := legacyHeader[strings.TrimSpace(h)]; ok {
			cols[f] = i
		}
	}
	for _, f := range RequiredFields {
		if _, ok := cols[f]; !ok {
			return nil, fmt.Errorf("legacy: 表头缺少字段 %s", f)
		}
	}

	var out []Row
	for r, raw := range rows[1:] {
		if isEmptyRow(raw) {
			continue
		}
		row, err := parseLegacyRow(raw, cols)
		if err != nil {
			return nil, fmt.Errorf("legacy: 第 %d 行: %w", r+2, err)
		}
		out = append(out, row)
	}
	return out, nil
}

func parseLegacyRow(raw []string, cols map[Field]int) (Row, error) {
	get := func(f Field) string {
		i := cols[f]
		if i >= len(raw) {
			return ""
		}
		return strings.TrimSpace(raw[i])
	}

	var row Row
	row.OrderID = get(FieldOrderID)
	row.Product = get(FieldProduct)
	row.Note = get(FieldNote)

	d, err := time.Parse("2006-01-02", get(FieldDate))
	if err != nil {
		return Row{}, fmt.Errorf("日期 %q", get(FieldDate))
	}
	row.Date = FormatDate(d)

	if row.UnitPrice, err = strconv.ParseFloat(get(FieldUnitPrice), 64); err != nil {
		return Row{}, fmt.Errorf("单价 %q", get(FieldUnitPrice))
	}
	if row.Amount, err = strconv.ParseFloat(get(FieldAmount), 64); err != nil {
		return Row{}, fmt.Errorf("金额 %q", get(FieldAmount))
	}
	qty, err := strconv.ParseFloat(get(FieldQty), 64)
	if err != nil {
		return Row{}, fmt.Errorf("数量 %q", get(FieldQty))
	}
	row.Qty = int(qty)
	return row, nil
}
