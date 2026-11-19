package parser_test

import (
	"math"
	"testing"

	"github.com/jxu-hub/invoice-parser/internal/parser"
)

func TestParseMoney(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		bad  bool
	}{
		{"1234.5", 1234.5, false},
		{"1,234.50", 1234.5, false},
		{"¥1,234.50", 1234.5, false},
		{"￥ 1234.5元", 1234.5, false},
		{"1,234.5元", 1234.5, false},
		{"(120.00)", -120, false},
		{"－120.5", -120.5, false},
		{"负88", -88, false},
		{"1 234.5", 1234.5, false},
		{"399", 399, false},
		{"398.0", 398, false},
		{"8,800,000.00", 8800000, false},
		{"", 0, true},
		{"合计", 0, true},
		{"１２３", 0, true},
	}
	for _, c := range cases {
		got, err := parser.ParseMoney(c.in)
		if c.bad {
			if err == nil {
				t.Errorf("ParseMoney(%q) 应该失败，却得到 %v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseMoney(%q) 失败: %v", c.in, err)
			continue
		}
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("ParseMoney(%q) = %v, 期望 %v", c.in, got, c.want)
		}
	}
}

func TestParseDate(t *testing.T) {
	cases := []struct {
		in   string
		want string
		bad  bool
	}{
		{"2024-01-05", "2024-01-05", false},
		{"2024-1-5", "2024-01-05", false},
		{"2024/1/5", "2024-01-05", false},
		{"2024/01/05", "2024-01-05", false},
		{"2024.01.05", "2024-01-05", false},
		{"2024年1月5日", "2024-01-05", false},
		{"2024年01月05日", "2024-01-05", false},
		{"20240105", "2024-01-05", false},
		{"05-Jan-24", "2024-01-05", false},
		{"2024-01-05 00:00:00", "2024-01-05", false},
		{"", "", true},
		{"昨天", "", true},
		{"2024-13-40", "", true},
		// 8 位单号在 20060102 布局下会被解析成公元 0 年，必须挡住，
		// 否则单号列会被判成日期列，前导零也会丢。
		{"00001101", "", true},
	}
	for _, c := range cases {
		got, err := parser.ParseDate(c.in)
		if c.bad {
			if err == nil {
				t.Errorf("ParseDate(%q) 应该失败", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseDate(%q) 失败: %v", c.in, err)
			continue
		}
		if s := parser.FormatDate(got); s != c.want {
			t.Errorf("ParseDate(%q) = %s, 期望 %s", c.in, s, c.want)
		}
	}
}

// TestClassifyOrder 锁住类型判断的优先顺序：
// 8 位纯数字先判日期再判单号，单号要求有前导零或足够长。
func TestClassifyOrder(t *testing.T) {
	cases := map[string]parser.Kind{
		"20240105":   parser.KindDate,
		"00001234":   parser.KindID,
		"0000000001": parser.KindID,
		"123456":     parser.KindMoney,
		"1234.5":     parser.KindMoney,
		"¥1,234.5":   parser.KindMoney,
		"帆布包":        parser.KindText,
		"":           parser.KindEmpty,
	}
	for in, want := range cases {
		if got := parser.Classify(in); got != want {
			t.Errorf("Classify(%q) = %v, 期望 %v", in, got, want)
		}
	}
}

func TestInferKindNeedsThreeSamples(t *testing.T) {
	if k, c := parser.InferKind([]string{"1", "2"}); k != parser.KindEmpty || c != 0 {
		t.Fatalf("样本不足 3 个应该返回空类型，实际 %v %.2f", k, c)
	}
	k, c := parser.InferKind([]string{"1.5", "2.5", "3.5", "4.5"})
	if k != parser.KindMoney || c != 1 {
		t.Fatalf("全数字列应该是 money 且一致性 1.0，实际 %v %.2f", k, c)
	}
	// 一列里混进一个文本，一致性下降到 0.75
	k, c = parser.InferKind([]string{"1.5", "2.5", "3.5", "待定"})
	if k != parser.KindMoney {
		t.Fatalf("多数派应该是 money，实际 %v", k)
	}
	if math.Abs(c-0.75) > 1e-9 {
		t.Fatalf("一致性应该是 0.75，实际 %.2f", c)
	}
}
