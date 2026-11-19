package sheet_test

import (
	"testing"

	"github.com/jxu-hub/invoice-parser/internal/sheet"
	"github.com/jxu-hub/invoice-parser/internal/xlsx"
)

// TestFlattenFillsMergedCells 是合并单元格的核心用例：
// 合并区域只有左上角有值，展平后每个格子都要有值，
// 并且这一行要被标记成"跨列合并"（组表头），供表头识别跳过。
func TestFlattenFillsMergedCells(t *testing.T) {
	rows := [][]string{
		{"订单信息", "", "商品", "金额与数量", "", "", "备注"},
		{"订单编号", "下单日期", "商品名称", "单价", "数量", "金额", "备注"},
		{"00000001", "2024-01-05", "帆布包", "39.9", "2", "79.8", ""},
	}
	merges := []xlsx.Range{
		{R1: 0, C1: 0, R2: 0, C2: 1},
		{R1: 0, C1: 3, R2: 0, C2: 5},
	}

	tbl := sheet.Flatten(rows, merges)

	if got := tbl.Rows[0][1]; got != "订单信息" {
		t.Fatalf("合并区域右侧应该填上左上角的值，实际 %q", got)
	}
	if got := tbl.Rows[0][4]; got != "金额与数量" {
		t.Fatalf("合并区域中间格应该填上左上角的值，实际 %q", got)
	}
	if !tbl.MergedRow[0] {
		t.Fatal("第 0 行有跨列合并，应该被标记为组表头")
	}
	if tbl.MergedRow[1] {
		t.Fatal("第 1 行没有合并，不该被标记")
	}
	if tbl.Spanning[0] != 3 {
		t.Fatalf("最大合并宽度应该是 3，实际 %d", tbl.Spanning[0])
	}
}

// TestFlattenDoesNotOverwrite 合并区域不能盖掉已经存在的值。
func TestFlattenDoesNotOverwrite(t *testing.T) {
	rows := [][]string{
		{"A", "已有值"},
		{"C", "D"},
	}
	merges := []xlsx.Range{{R1: 0, C1: 0, R2: 0, C2: 1}}
	tbl := sheet.Flatten(rows, merges)
	if got := tbl.Rows[0][1]; got != "已有值" {
		t.Fatalf("不该覆盖已有值，实际 %q", got)
	}
}

func TestFlattenRectangular(t *testing.T) {
	rows := [][]string{
		{"a", "b", "c"},
		{"d"},
	}
	tbl := sheet.Flatten(rows, nil)
	if tbl.Width() != 3 {
		t.Fatalf("宽度应该是 3，实际 %d", tbl.Width())
	}
	if tbl.Rows[1][2] != "" {
		t.Fatalf("短行应该补齐成空串，实际 %q", tbl.Rows[1][2])
	}
}
