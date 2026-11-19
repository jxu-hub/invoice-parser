// Package golden 负责读测试清单、读 golden 答案、比较解析结果。
//
// baseline 脚本和单元测试共用这一套比较逻辑：如果两边各写一份，
// 很容易出现"脚本说过了、测试说挂了"这种自欺欺人的情况。
package golden

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/jxu-hub/invoice-parser/internal/parser"
)

// Sheet 是清单里的一张表。
type Sheet struct {
	File   string `json:"file"`   // relative to testdata/
	Golden string `json:"golden"` // relative to testdata/
	Label  string `json:"label"`  // clean / header / merged / money / date / other
}

// Manifest 是 testdata/manifest.json。
type Manifest struct {
	Sheets []Sheet `json:"sheets"`
}

// Load 读取清单。返回的路径已经拼成可直接打开的路径。
func Load(manifestPath string) ([]Sheet, error) {
	b, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	var mf Manifest
	if err := json.Unmarshal(b, &mf); err != nil {
		return nil, fmt.Errorf("golden: 解析 %s: %w", manifestPath, err)
	}
	return mf.Sheets, nil
}

// Read 读取一张表的期望结果。
func Read(path string) ([]parser.Row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("golden: %s 是空文件", path)
	}
	out := make([]parser.Row, 0, len(rows)-1)
	for i, r := range rows[1:] {
		if len(r) < 7 {
			return nil, fmt.Errorf("golden: %s 第 %d 行列数不足", path, i+2)
		}
		price, err := strconv.ParseFloat(r[3], 64)
		if err != nil {
			return nil, fmt.Errorf("golden: %s 第 %d 行单价: %w", path, i+2, err)
		}
		qty, err := strconv.Atoi(r[4])
		if err != nil {
			return nil, fmt.Errorf("golden: %s 第 %d 行数量: %w", path, i+2, err)
		}
		amount, err := strconv.ParseFloat(r[5], 64)
		if err != nil {
			return nil, fmt.Errorf("golden: %s 第 %d 行金额: %w", path, i+2, err)
		}
		out = append(out, parser.Row{
			OrderID:   r[0],
			Date:      r[1],
			Product:   r[2],
			UnitPrice: price,
			Qty:       qty,
			Amount:    amount,
			Note:      r[6],
		})
	}
	return out, nil
}

// Compare 逐行比较，返回第一个差异。nil 表示整表完全一致。
// 判定标准是"整表一次过"：行数一致，且每行每个字段都一致。
func Compare(want, got []parser.Row) error {
	if len(want) != len(got) {
		return fmt.Errorf("行数不符: 期望 %d 行, 实际 %d 行", len(want), len(got))
	}
	for i := range want {
		if want[i] != got[i] {
			return fmt.Errorf("第 %d 条数据不符:\n  期望 %v\n  实际 %v", i+1, want[i].Fields(), got[i].Fields())
		}
	}
	return nil
}
