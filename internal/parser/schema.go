package parser

import (
	"strings"
)

// Field 是规范化之后的目标字段。解析器只认这几个字段，
// 一张表至少要能认出 6 个必填字段，否则判为 schema 不匹配。
type Field string

const (
	FieldOrderID   Field = "order_id"
	FieldDate      Field = "order_date"
	FieldProduct   Field = "product"
	FieldUnitPrice Field = "unit_price"
	FieldQty       Field = "qty"
	FieldAmount    Field = "amount"
	FieldNote      Field = "note"
)

// RequiredFields 是一张销售明细表必须能认出来的字段。
var RequiredFields = []Field{
	FieldOrderID, FieldDate, FieldProduct,
	FieldUnitPrice, FieldQty, FieldAmount,
}

// fieldAliases 是列名别名表。真实表里的列名几乎不会完全统一，
// 同一家公司不同系统导出的表，列名能有三四种写法。
var fieldAliases = map[Field][]string{
	FieldOrderID:   {"订单编号", "订单号", "单号", "订单id", "订单编码", "编号", "orderid", "orderno"},
	FieldDate:      {"下单日期", "日期", "下单时间", "订单日期", "成交日期", "统计日期", "date"},
	FieldProduct:   {"商品名称", "商品", "品名", "产品", "商品名", "货品名称", "product"},
	FieldUnitPrice: {"单价", "价格", "商品单价", "售价", "成交价", "unitprice", "price"},
	FieldQty:       {"数量", "件数", "购买数量", "数目", "销量", "qty", "quantity"},
	FieldAmount:    {"金额", "小计", "总额", "总金额", "订单金额", "金额合计", "amount", "total"},
	FieldNote:      {"备注", "说明", "备注信息", "note", "remark"},
}

// aliasIndex 是反查表，构建一次反复用。
var aliasIndex = func() map[string]Field {
	m := make(map[string]Field, 64)
	for f, names := range fieldAliases {
		for _, n := range names {
			m[NormalizeHeader(n)] = f
		}
	}
	return m
}()

// NormalizeHeader 把列名收敛成可比较的形式：
// 去空白、去全角符号、去括号里的补充说明、转小写。
func NormalizeHeader(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Map(func(r rune) rune {
		switch r {
		case '（', '(':
			return '('
		case '）', ')':
			return ')'
		case '：', ':':
			return ':'
		case '　', 0x00A0:
			return ' '
		}
		return r
	}, s)
	// 去掉 "(必填)" "（元）" 这类后缀说明
	if i := strings.Index(s, "("); i > 0 {
		s = s[:i]
	}
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, ":", "")
	s = strings.ReplaceAll(s, "\n", "")
	return strings.ToLower(strings.TrimSpace(s))
}

// MapHeader 把表头文字映射成字段。
func MapHeader(header string) (Field, bool) {
	f, ok := aliasIndex[NormalizeHeader(header)]
	return f, ok
}

// Column 是解析出来的一列。
type Column struct {
	Index      int
	Name       string
	Kind       Kind
	Field      Field
	Confidence float64 // 该列类型推断的一致性
	Matched    bool    // 是否由列名映射命中（false 表示按类型兜底认的）
}

// Schema 是一张表的列结构。
type Schema struct {
	HeaderRow int
	Score     float64 // 表头分裂度，越小越像真表头
	Columns   []Column
}

// ColumnOf 按字段取列。
func (s Schema) ColumnOf(f Field) (Column, bool) {
	for _, c := range s.Columns {
		if c.Field == f {
			return c, true
		}
	}
	return Column{}, false
}

// Missing 返回没能认出来的字段。
func (s Schema) Missing() []Field {
	var out []Field
	for _, f := range RequiredFields {
		if _, ok := s.ColumnOf(f); !ok {
			out = append(out, f)
		}
	}
	return out
}
