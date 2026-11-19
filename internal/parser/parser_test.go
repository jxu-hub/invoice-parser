package parser_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jxu-hub/invoice-parser/internal/golden"
	"github.com/jxu-hub/invoice-parser/internal/parser"
	"github.com/jxu-hub/invoice-parser/internal/sheet"
)

const testdata = "../../testdata"

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

// TestGolden 是仓库的主闸门：200 张表全部要一次过。
// 任何改动只要让其中一张表的行数或某个字段变了，这里就会红。
//
// fixture 已提交进仓库，不要为了让它变绿去改 golden。
func TestGolden(t *testing.T) {
	sheets := loadManifest(t)
	if len(sheets) != 200 {
		t.Fatalf("清单里应该有 200 张表，实际 %d 张", len(sheets))
	}

	for _, s := range sheets {
		s := s
		name := strings.TrimSuffix(filepath.Base(s.File), ".xlsx")
		t.Run(s.Label+"/"+name, func(t *testing.T) {
			res, err := parseFile(t, s.File)
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			want, err := golden.Read(filepath.Join(testdata, s.Golden))
			if err != nil {
				t.Fatalf("读 golden 失败: %v", err)
			}
			if err := golden.Compare(want, res.Rows); err != nil {
				t.Fatalf("%v", err)
			}
			if s.Label == "clean" && len(res.Isolated) != 0 {
				t.Fatalf("干净表不应该有被隔离的行，实际 %d 行: %v", len(res.Isolated), res.Isolated[0].Reason)
			}
		})
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

// TestIsolatedRowKeepsTableAlive 是这次改造的核心行为：
// 一行坏掉，不能让整张表失败。
func TestIsolatedRowKeepsTableAlive(t *testing.T) {
	tbl := sheet.Flatten([][]string{
		{"订单编号", "下单日期", "商品名称", "单价", "数量", "金额", "备注"},
		{"00000001", "2024-01-05", "帆布包", "39.9", "2", "79.8", ""},
		{"00000002", "昨天下午", "保温杯", "39.9", "1", "39.9", ""},
		{"00000003", "2024-01-07", "毛线帽", "15.8", "3", "47.4", "补发"},
	}, nil)

	res, err := parser.Parse(tbl)
	if err != nil {
		t.Fatalf("单行坏掉不应该让整表失败: %v", err)
	}
	if len(res.Rows) != 2 {
		t.Fatalf("应该保住 2 条好数据，实际 %d 条", len(res.Rows))
	}
	if len(res.Isolated) != 1 {
		t.Fatalf("应该有 1 行被隔离，实际 %d 行", len(res.Isolated))
	}
	if res.Isolated[0].Line != 3 {
		t.Fatalf("隔离行应该报第 3 行，实际第 %d 行", res.Isolated[0].Line)
	}
	if !strings.Contains(res.Isolated[0].Reason, "日期") {
		t.Fatalf("隔离原因应该指明是日期，实际 %q", res.Isolated[0].Reason)
	}
}

// TestNoiseRowsSkipped 合计行、页码、脚注不能进结果，也不该进隔离区。
func TestNoiseRowsSkipped(t *testing.T) {
	tbl := sheet.Flatten([][]string{
		{"订单编号", "下单日期", "商品名称", "单价", "数量", "金额", "备注"},
		{"00000001", "2024-01-05", "帆布包", "39.9", "2", "79.8", ""},
		{"合计", "", "", "", "", "79.8", ""},
		{"注：本表由系统导出", "", "", "", "", "", ""},
		{"第 1 页 / 共 1 页", "", "", "", "", "", ""},
	}, nil)

	res, err := parser.Parse(tbl)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("应该只有 1 条数据，实际 %d 条", len(res.Rows))
	}
	if len(res.Isolated) != 0 {
		t.Fatalf("噪音行不该进隔离区，实际 %d 行", len(res.Isolated))
	}
	if res.Skipped != 3 {
		t.Fatalf("应该跳过 3 行噪音，实际 %d 行", res.Skipped)
	}
}

func TestSchemaDiff(t *testing.T) {
	prev := parser.Schema{Columns: []parser.Column{
		{Index: 0, Name: "订单编号", Kind: parser.KindID, Field: parser.FieldOrderID},
		{Index: 1, Name: "下单日期", Kind: parser.KindDate, Field: parser.FieldDate},
	}}
	cur := parser.Schema{Columns: []parser.Column{
		{Index: 0, Name: "订单编号", Kind: parser.KindID, Field: parser.FieldOrderID},
		{Index: 1, Name: "下单日期", Kind: parser.KindText, Field: parser.FieldDate},
		{Index: 2, Name: "备注", Kind: parser.KindText, Field: parser.FieldNote},
	}}
	changes := parser.Diff(prev, cur)
	if len(changes) != 2 {
		t.Fatalf("应该识别出 2 处变化，实际 %d 处: %v", len(changes), changes)
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
