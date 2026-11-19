// Package sheet 负责把 xlsx 的原始网格整理成"可以往下解析"的表格：
// 去掉首尾空白、补齐成矩形。合并单元格的处理等真被绊到再加。
package sheet

import (
	"strings"

	"github.com/jxu-hub/invoice-parser/internal/xlsx"
)

// Table 是整理后的表格。
type Table struct {
	// Rows 是补齐后的矩形数据。
	Rows [][]string
}

// Load 读取并整理一个 xlsx 文件。
func Load(file string) (*Table, error) {
	s, err := xlsx.Open(file)
	if err != nil {
		return nil, err
	}
	return Flatten(s.Rows), nil
}

// Flatten 清理空白并补齐成矩形。
func Flatten(rows [][]string) *Table {
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
	return &Table{Rows: out}
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
