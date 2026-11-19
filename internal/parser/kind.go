package parser

import (
	"strings"
	"unicode"
)

// Kind 是单元格的语义类型。推断出来的 Kind 有两个用处：
//   - 算表头的分裂度（同一列类型越一致，这一行越像真表头）
//   - 列名匹配不上时，按类型兜底认列
type Kind int

const (
	KindEmpty Kind = iota
	KindText
	KindMoney
	KindDate
	KindID
)

func (k Kind) String() string {
	switch k {
	case KindMoney:
		return "money"
	case KindDate:
		return "date"
	case KindID:
		return "id"
	case KindText:
		return "text"
	default:
		return "empty"
	}
}

// Classify 判断单个单元格的类型。顺序很重要：
// 先认日期（20240105 这种 8 位纯数字既像日期又像编号），再认编号，最后才是数字。
func Classify(v string) Kind {
	s := strings.TrimSpace(v)
	if s == "" {
		return KindEmpty
	}
	if IsDate(s) {
		return KindDate
	}
	if IsID(s) {
		return KindID
	}
	if _, err := ParseMoney(s); err == nil {
		return KindMoney
	}
	return KindText
}

// IsID 判断是否像一个单号：纯数字、至少 6 位，且有前导零或足够长。
// 前导零是关键——很多系统导出的单号是 000123 这种，按数字解析会把零吃掉。
func IsID(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 6 {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) || r > unicode.MaxASCII {
			return false
		}
	}
	if strings.HasPrefix(s, "0") {
		return true
	}
	return len(s) >= 10
}

// InferKind 对一列取值做投票，返回多数类型和一致性（0~1）。
// 少于 3 个样本时返回 KindEmpty / 0，调用方应当忽略这一列。
func InferKind(vals []string) (Kind, float64) {
	counts := map[Kind]int{}
	total := 0
	for _, v := range vals {
		k := Classify(v)
		if k == KindEmpty {
			continue
		}
		counts[k]++
		total++
	}
	if total < 3 {
		return KindEmpty, 0
	}
	best, bestN := KindEmpty, 0
	for _, k := range []Kind{KindDate, KindID, KindMoney, KindText} {
		if counts[k] > bestN {
			best, bestN = k, counts[k]
		}
	}
	return best, float64(bestN) / float64(total)
}
