package parser

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
