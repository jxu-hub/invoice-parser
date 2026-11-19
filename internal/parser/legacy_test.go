package parser_test

import (
	"path/filepath"
	"testing"

	"github.com/jxu-hub/invoice-parser/internal/golden"
	"github.com/jxu-hub/invoice-parser/internal/parser"
	"github.com/jxu-hub/invoice-parser/internal/sheet"
	"github.com/jxu-hub/invoice-parser/internal/xlsx"
)

// legacyFailsByClass 是当前解析器在 200 张表上的失败分布。
// 这组数字是基线：任何改动都要先跑一遍 baseline 脚本，把新的分布贴出来。
var legacyFailsByClass = map[string]int{
	"clean":  0,
	"header": 21,
	"merged": 18,
	"money":  14,
	"date":   7,
	"other":  3,
}

const expectedPasses = 137

func TestBaselineIs137Of200(t *testing.T) {
	sheets, err := golden.Load(filepath.Join("..", "..", "testdata", "manifest.json"))
	if err != nil {
		t.Fatalf("读取清单失败: %v", err)
	}

	got := map[string]int{}
	pass, fail := 0, 0
	for _, s := range sheets {
		want, err := golden.Read(filepath.Join("..", "..", "testdata", s.Golden))
		if err != nil {
			t.Fatalf("%s: %v", s.Golden, err)
		}
		src, err := xlsx.Open(filepath.Join("..", "..", "testdata", s.File))
		if err != nil {
			t.Fatalf("%s: %v", s.File, err)
		}
		rows, err := parser.ParseLegacy(src.Rows)
		if err == nil {
			err = golden.Compare(want, rows)
		}
		if err != nil {
			got[s.Label]++
			fail++
			continue
		}
		pass++
	}

	if pass != expectedPasses {
		t.Fatalf("基线应该是 %d/%d，实际 %d/%d", expectedPasses, len(sheets), pass, len(sheets))
	}
	if fail != len(sheets)-expectedPasses {
		t.Fatalf("失败数应为 %d，实际 %d", len(sheets)-expectedPasses, fail)
	}
	for class, want := range legacyFailsByClass {
		if got[class] != want {
			t.Errorf("在 %s 类的失败数应为 %d，实际 %d", class, want, got[class])
		}
	}
}

// TestNoRegressionAgainstLegacy 是"不许把以前能过的表弄挂"的自动化版本。
// 只要旧实现能过、而新实现过不了，就是回归，必须修。
func TestNoRegressionAgainstLegacy(t *testing.T) {
	sheets, err := golden.Load(filepath.Join("..", "..", "testdata", "manifest.json"))
	if err != nil {
		t.Fatalf("读取清单失败: %v", err)
	}
	for _, s := range sheets {
		want, err := golden.Read(filepath.Join("..", "..", "testdata", s.Golden))
		if err != nil {
			t.Fatalf("%s: %v", s.Golden, err)
		}
		src, err := xlsx.Open(filepath.Join("..", "..", "testdata", s.File))
		if err != nil {
			t.Fatalf("%s: %v", s.File, err)
		}
		oldRows, oldErr := parser.ParseLegacy(src.Rows)
		if oldErr == nil {
			oldErr = golden.Compare(want, oldRows)
		}
		if oldErr != nil {
			continue // 旧实现本来就过不了，不构成回归
		}

		tbl, err := sheet.Load(filepath.Join("..", "..", "testdata", s.File))
		if err != nil {
			t.Fatalf("%s: %v", s.File, err)
		}
		res, err := parser.Parse(tbl)
		if err != nil {
			t.Fatalf("%s: 旧实现能过、新实现却失败: %v", s.File, err)
		}
		if err := golden.Compare(want, res.Rows); err != nil {
			t.Fatalf("%s: 旧实现能过、新实现结果不一致: %v", s.File, err)
		}
	}
}
