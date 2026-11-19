package xlsx_test

import (
	"path/filepath"
	"testing"

	"github.com/jxu-hub/invoice-parser/internal/xlsx"
)

// TestOpenRealFile 直接读一张生成的 xlsx，覆盖 zip、workbook 关系表、
// sharedStrings、合并单元格这几条路径。用 inlineStr 的那批文件由
// 生成器控制，另有 golden 测试全量覆盖。
func TestOpenRealFile(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "raw", "shop06-2024-06-006.xlsx")
	s, err := xlsx.Open(path)
	if err != nil {
		t.Fatalf("打开失败: %v", err)
	}
	if len(s.Rows) == 0 {
		t.Fatal("没有读到任何行")
	}
	if s.Name == "" {
		t.Fatal("工作表名不该为空")
	}
	if len(s.Merges) != 2 {
		t.Fatalf("这张表应该有 2 个合并区域，实际 %d", len(s.Merges))
	}
	if s.Merges[0].R1 != 0 || s.Merges[0].C1 != 0 || s.Merges[0].C2 != 1 {
		t.Fatalf("合并区域解析错误: %+v", s.Merges[0])
	}
}

func TestOpenMissingFile(t *testing.T) {
	if _, err := xlsx.Open(filepath.Join("..", "..", "testdata", "nope.xlsx")); err == nil {
		t.Fatal("打开不存在的文件应该失败")
	}
}
