package parser

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseMoney 解析金额。
// 目前只当它是纯数字：带 ¥、千分位、括号负数的写法会直接失败。
func ParseMoney(raw string) (float64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("金额为空")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("金额 %q 无法解析", raw)
	}
	return v, nil
}

// FormatMoney 输出成稳定的字符串形式，测试和 golden 文件都用它，
// 避免 398.0 和 398 这种无意义的 diff。
func FormatMoney(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
