// Package sheet 负责把 xlsx 的原始网格整理成"可以往下解析"的表格：
// 展平合并单元格、去掉首尾空白、记录哪些行是跨列合并的组表头。
//
// 合并单元格是这类表格最主要的坑：合并区域只有左上角有值，
// 其他格子读出来是空串。不展平，后面的表头识别和列推断全部会错位。
package sheet

import (
	"strings"

	"github.com/jxu-hub/invoice-parser/internal/xlsx"
)

// Table 是展平后的表格。
type Table struct {
	// Rows 是补全合并区域后的矩形数据。
	Rows [][]string
	// MergedRow[r] 为 true 表示第 r 行有跨列合并，通常是标题行或组表头。
	MergedRow []bool
	// Spanning[r] 记录第 r 行里跨列合并的最大宽度（0 表示没有）。
	Spanning []int
}

// Load 读取并展平一个 xlsx 文件。
func Load(file string) (*Table, error) {
	s, err := xlsx.Open(file)
	if err != nil {
		return nil, err
	}
	return Flatten(s.Rows, s.Merges), nil
}

// Flatten 展平合并单元格并清理空白。
func Flatten(rows [][]string, merges []xlsx.Range) *Table {
	width := 0
	for _, r := range rows {
		if len(r) > width {
			width = len(r)
		}
	}
	out := make([][]string, len(rows))
	for i, r := range rows {
		cells := make([]string, width)
		for j := 0; j < width; j++ {
			if j < len(r) {
				cells[j] = strings.TrimSpace(r[j])
			}
		}
		out[i] = cells
	}

	t := &Table{
		Rows:      out,
		MergedRow: make([]bool, len(rows)),
		Spanning:  make([]int, len(rows)),
	}

	for _, m := range merges {
		if m.R1 < 0 || m.R1 >= len(out) || m.C1 < 0 || m.C1 >= width {
			continue
		}
		val := out[m.R1][m.C1]
		for r := m.R1; r <= m.R2 && r < len(out); r++ {
			for c := m.C1; c <= m.C2 && c < width; c++ {
				// 只在目标格为空时填充，避免覆盖真实数据。
				if out[r][c] == "" {
					out[r][c] = val
				}
			}
		}
		if m.R1 == m.R2 && m.C2 > m.C1 {
			t.MergedRow[m.R1] = true
			if w := m.C2 - m.C1 + 1; w > t.Spanning[m.R1] {
				t.Spanning[m.R1] = w
			}
		}
	}
	return t
}

// NonEmpty 返回一行里非空单元格数量。
func (t *Table) NonEmpty(row int) int {
	if row < 0 || row >= len(t.Rows) {
		return 0
	}
	n := 0
	for _, c := range t.Rows[row] {
		if c != "" {
			n++
		}
	}
	return n
}

// Width 返回表格列数。
func (t *Table) Width() int {
	if len(t.Rows) == 0 {
		return 0
	}
	return len(t.Rows[0])
}
