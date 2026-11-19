package parser_test

import (
	"path/filepath"
	"testing"

	"github.com/jxu-hub/invoice-parser/internal/golden"
	"github.com/jxu-hub/invoice-parser/internal/parser"
	"github.com/jxu-hub/invoice-parser/internal/sheet"
	"github.com/jxu-hub/invoice-parser/internal/xlsx"
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

// TestMergedGroupHeaderSkipped 覆盖"组表头"这条规则：
// 跨列合并的行一定是组表头，真表头在下面，不能靠打分去赌。
func TestMergedGroupHeaderSkipped(t *testing.T) {
	tbl := sheet.Flatten([][]string{
		{"订单信息", "", "商品", "金额与数量", "", "", "备注"},
		{"订单编号", "下单日期", "商品名称", "单价", "数量", "金额", "备注"},
		{"00000001", "2024-01-05", "帆布包", "39.9", "2", "79.8", ""},
		{"00000002", "2024-01-06", "保温杯", "68", "1", "68", ""},
		{"00000003", "2024-01-07", "毛线帽", "15.8", "3", "47.4", ""},
	}, []xlsx.Range{{R1: 0, C1: 0, R2: 0, C2: 1}, {R1: 0, C1: 3, R2: 0, C2: 5}})

	if !tbl.MergedRow[0] {
		t.Fatal("第 0 行有跨列合并，应该被标记为组表头")
	}
	if got := tbl.Rows[0][1]; got != "订单信息" {
		t.Fatalf("合并区域右侧应该填上左上角的值，实际 %q", got)
	}
	row, _, ok := parser.DetectHeader(tbl)
	if !ok || row != 1 {
		t.Fatalf("应该跳过组表头选第 2 行，实际选中第 %d 行", row+1)
	}
}

// TestVerticalMergeFilled 覆盖数据区的纵向合并：同一个商品跨几行下单时，
// 导出表里商品名往往只写一次。不展平，后面几行的商品名就是空的。
func TestVerticalMergeFilled(t *testing.T) {
	tbl := sheet.Flatten([][]string{
		{"订单编号", "下单日期", "商品名称", "单价", "数量", "金额", "备注"},
		{"00000001", "2024-01-05", "帆布包", "39.9", "2", "79.8", ""},
		{"00000002", "2024-01-06", "", "39.9", "1", "39.9", ""},
		{"00000003", "2024-01-07", "", "39.9", "3", "119.7", ""},
	}, []xlsx.Range{{R1: 1, C1: 2, R2: 3, C2: 2}})

	res, err := parser.Parse(tbl)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(res.Rows) != 3 {
		t.Fatalf("应该有 3 条数据，实际 %d 条", len(res.Rows))
	}
	for i, r := range res.Rows {
		if r.Product != "帆布包" {
			t.Errorf("第 %d 行商品名应为帆布包，实际 %q", i+1, r.Product)
		}
	}
}
