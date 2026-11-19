package parser

import (
	"fmt"
	"strconv"
	"strings"
)

// currencyPrefixes 是导出表格里常见的货币符号，中英文都有。
var currencyPrefixes = []string{"¥", "￥", "$", "＄", "RMB", "CNY", "rmb", "cny"}

// currencySuffixes 是单位后缀，中文表里极常见（"1,234.5元"）。
var currencySuffixes = []string{"元", "块", "人民币", "整"}

// ParseMoney 把各种写法的金额解析成 float64。
//
// 支持：
//
//	"1234.5"  "1,234.50"  "¥1,234.50"  "￥ 1234.5元"  "1,234.5元"
//	"(120.00)"  → -120     会计里的括号负数
//	"－120.5"   "负120.5"   → -120.5  全角负号 / 中文负
//	"1 234.5"   全角空格、普通空格作为千分位分隔
func ParseMoney(raw string) (float64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("金额为空")
	}
	// 统一全角字符，省得后面逐个判断。
	s = strings.Map(func(r rune) rune {
		switch r {
		case '，':
			return ','
		case '．', '。':
			return '.'
		case '－', '—', '−':
			return '-'
		case '＋':
			return '+'
		case 0x00A0, 0x3000:
			return ' '
		}
		return r
	}, s)

	neg := false
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		neg = true
		s = s[1 : len(s)-1]
	}
	if strings.HasPrefix(s, "负") {
		neg = true
		s = strings.TrimPrefix(s, "负")
	}
	s = strings.TrimSpace(s)

	for _, p := range currencyPrefixes {
		if strings.HasPrefix(strings.ToUpper(s), strings.ToUpper(p)) {
			s = strings.TrimSpace(s[len(p):])
			break
		}
	}
	for _, suf := range currencySuffixes {
		if strings.HasSuffix(s, suf) {
			s = strings.TrimSpace(strings.TrimSuffix(s, suf))
			break
		}
	}

	// 去掉千分位和内部空格：1,234.5 / 1 234.5
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, " ", "")
	if s == "" {
		return 0, fmt.Errorf("金额为空")
	}
	if strings.HasPrefix(s, "-") {
		neg = !neg
		s = s[1:]
	} else if strings.HasPrefix(s, "+") {
		s = s[1:]
	}

	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("金额 %q 无法解析", raw)
	}
	if neg {
		v = -v
	}
	return v, nil
}

// FormatMoney 输出成稳定的字符串形式，测试和 golden 文件都用它，
// 避免 398.0 和 398 这种无意义的 diff。
func FormatMoney(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
