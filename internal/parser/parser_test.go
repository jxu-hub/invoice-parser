package parser_test

import (
	"path/filepath"
	"testing"

	"github.com/jxu-hub/invoice-parser/internal/golden"
	"github.com/jxu-hub/invoice-parser/internal/parser"
	"github.com/jxu-hub/invoice-parser/internal/sheet"
)

const testdata = "../../testdata"

// 当前版本的通过数。每合并一个 PR，这个数只许变大，改小要单独说明原因。
const currentPasses = 176

// expectedFails 是还剩几类没解决。合并 PR 时这张表要跟着往下减。
var expectedFails = map[string]int{
	"clean":  0,
	"header": 0,
	"merged": 0,
	"money":  14,
	"date":   7,
	"other":  3,
}

func loadManifest(t *testing.T) []golden.Sheet {
	t.Helper()
	sheets, err := golden.Load(filepath.Join(testdata, "manifest.json"))
	if err != nil {
		t.Fatalf("读取清单失败: %v", err)
	}
	return sheets
}

func parseFile(t *testing.T, name string) (*parser.Result, error) {
	t.Helper()
	tbl, err := sheet.Load(filepath.Join(testdata, name))
	if err != nil {
		return nil, err
	}
	return parser.Parse(tbl)
}

// TestGolden 是主闸门：逐表逐行比对，并锁住当前通过数。
func TestGolden(t *testing.T) {
	sheets := loadManifest(t)
	if len(sheets) != 200 {
		t.Fatalf("清单里应该有 200 张表，实际 %d 张", len(sheets))
	}

	pass := 0
	fails := map[string]int{}
	for _, s := range sheets {
		res, err := parseFile(t, s.File)
		if err == nil {
			want, gerr := golden.Read(filepath.Join(testdata, s.Golden))
			if gerr != nil {
				t.Fatalf("%s: %v", s.Golden, gerr)
			}
			err = golden.Compare(want, res.Rows)
		}
		if err != nil {
			fails[s.Label]++
			continue
		}
		pass++
	}

	if pass != currentPasses {
		t.Fatalf("通过数应该是 %d，实际 %d。改动只允许让这个数字变大。", currentPasses, pass)
	}
	for class, want := range expectedFails {
		if fails[class] != want {
			t.Errorf("%s 类失败数应为 %d，实际 %d", class, want, fails[class])
		}
	}
}

// TestHeaderRowOnDirtyTables 锁住"表头识别"这件事本身，
// 而不是只看最终结果对不对：这两类的表头都不在第 1 行。
func TestHeaderRowOnDirtyTables(t *testing.T) {
	for _, s := range loadManifest(t) {
		if s.Label != "header" && s.Label != "merged" {
			continue
		}
		res, err := parseFile(t, s.File)
		if err != nil {
			t.Fatalf("%s: %v", s.File, err)
		}
		if res.HeaderRow < 1 {
			t.Fatalf("%s(%s): 表头行应该是 1 行以后，实际 %d", s.File, s.Label, res.HeaderRow)
		}
		if missing := res.Schema.Missing(); len(missing) > 0 {
			t.Fatalf("%s: 表头认出来还是缺字段 %v", s.File, missing)
		}
	}
}

func TestMapHeaderAliases(t *testing.T) {
	cases := map[string]parser.Field{
		"订单编号":    parser.FieldOrderID,
		"单号":      parser.FieldOrderID,
		"订单号(必填)": parser.FieldOrderID,
		"下单日期":    parser.FieldDate,
		"日期":      parser.FieldDate,
		"金额（元）":   parser.FieldAmount,
		"小计":      parser.FieldAmount,
		"商品名称":    parser.FieldProduct,
		"数量":      parser.FieldQty,
	}
	for in, want := range cases {
		got, ok := parser.MapHeader(in)
		if !ok || got != want {
			t.Errorf("MapHeader(%q) = %v,%v 期望 %v", in, got, ok, want)
		}
	}
	if _, ok := parser.MapHeader("经手人"); ok {
		t.Errorf("不该把无关列名认成字段")
	}
}
