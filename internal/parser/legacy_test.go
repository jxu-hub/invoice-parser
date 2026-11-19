package parser_test

import (
	"path/filepath"
	"testing"

	"github.com/jxu-hub/invoice-parser/internal/golden"
	"github.com/jxu-hub/invoice-parser/internal/parser"
	"github.com/jxu-hub/invoice-parser/internal/sheet"
	"github.com/jxu-hub/invoice-parser/internal/xlsx"
)

// legacyFailsByClass 是 v1 的失败分布。这组数字是"改造前"的基线，
// 也是这个仓库唯一一处故意写死的期望值——它的作用是让"某天有人
// 顺手改了 v1"这种事立刻暴露，而不是悄悄发生。
var legacyFailsByClass = map[string]int{
	"clean":  0,
	"header": 21,
	"merged": 18,
	"money":  14,
	"date":   7,
	"other":  3,
}

const legacyPasses = 137

func TestLegacyBaselineIs137Of200(t *testing.T) {
	sheets := loadManifest(t)

	got := map[string]int{}
	pass, fail := 0, 0
	for _, s := range sheets {
		want, err := golden.Read(filepath.Join(testdata, s.Golden))
		if err != nil {
			t.Fatalf("%s: %v", s.Golden, err)
		}
		src, err := xlsx.Open(filepath.Join(testdata, s.File))
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

	if pass != legacyPasses {
		t.Fatalf("v1 基线应该是 %d/%d，实际 %d/%d", legacyPasses, len(sheets), pass, len(sheets))
	}
	if fail != len(sheets)-legacyPasses {
		t.Fatalf("v1 失败数应为 %d，实际 %d", len(sheets)-legacyPasses, fail)
	}
	for class, want := range legacyFailsByClass {
		if got[class] != want {
			t.Errorf("v1 在 %s 类的失败数应为 %d，实际 %d", class, want, got[class])
		}
	}
}

// TestNoRegressionAgainstLegacy 是"不许把以前能过的表弄挂"的自动化版本。
// 只要 v1 能过、而新实现过不了，就是回归，必须修。
func TestNoRegressionAgainstLegacy(t *testing.T) {
	for _, s := range loadManifest(t) {
		want, err := golden.Read(filepath.Join(testdata, s.Golden))
		if err != nil {
			t.Fatalf("%s: %v", s.Golden, err)
		}
		src, err := xlsx.Open(filepath.Join(testdata, s.File))
		if err != nil {
			t.Fatalf("%s: %v", s.File, err)
		}
		legacyRows, legacyErr := parser.ParseLegacy(src.Rows)
		if legacyErr == nil {
			legacyErr = golden.Compare(want, legacyRows)
		}
		if legacyErr != nil {
			continue // v1 本来就过不了，不构成回归
		}

		tbl, err := sheet.Load(filepath.Join(testdata, s.File))
		if err != nil {
			t.Fatalf("%s: %v", s.File, err)
		}
		res, err := parser.Parse(tbl)
		if err != nil {
			t.Fatalf("%s: v1 能过、新实现却失败: %v", s.File, err)
		}
		if err := golden.Compare(want, res.Rows); err != nil {
			t.Fatalf("%s: v1 能过、新实现结果不一致: %v", s.File, err)
		}
	}
}
