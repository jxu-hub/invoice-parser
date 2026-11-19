// Package parser 把表格行规范化成 orders 字段。
package parser

import (
	"fmt"
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

func isEmptyRow(raw []string) bool {
	for _, v := range raw {
		if v != "" {
			return false
		}
	}
	return true
}
