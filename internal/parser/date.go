package parser

import (
	"fmt"
	"strings"
	"time"
)

// dateLayouts 是实际表格里出现过的日期写法，按"最常见优先"排列。
// 顺序有意义：同一个字符串可能被多种 layout 接受，先匹配先返回。
var dateLayouts = []string{
	"2006-01-02",
	"2006-1-2",
	"2006/01/02",
	"2006/1/2",
	"2006.01.02",
	"2006.1.2",
	"2006年1月2日",
	"2006年01月02日",
	"20060102",
	"02-Jan-2006",
	"02-Jan-06",
	"2-Jan-06",
	"02/Jan/2006",
	"2006-01-02 15:04:05",
	"2006/1/2 15:04:05",
}

// ParseDate 把各种写法的日期解析成 date（时分秒归零）。
// 返回的时间统一用 UTC 定位，避免跨时区跑批时 golden 文件出现漂移。
func ParseDate(raw string) (time.Time, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, fmt.Errorf("日期为空")
	}
	s = strings.Map(func(r rune) rune {
		switch r {
		case '／':
			return '/'
		case '．', '。':
			return '.'
		case 0x00A0, 0x3000:
			return ' '
		}
		return r
	}, s)

	for _, layout := range dateLayouts {
		t, err := time.Parse(layout, s)
		if err != nil {
			continue
		}
		// 年份必须落在合理区间：00001101 这种 8 位单号在 "20060102" 布局下
		// 会被解析成公元 0 年 11 月 1 日，不挡住就会把单号列判成日期列。
		if t.Year() < 1900 || t.Year() > 2100 {
			continue
		}
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), nil
	}
	return time.Time{}, fmt.Errorf("日期 %q 无法解析", raw)
}

// IsDate 判断字符串能否被解析成日期。
func IsDate(s string) bool {
	_, err := ParseDate(s)
	return err == nil
}

// FormatDate 是写入 golden / 输出时的唯一日期格式。
func FormatDate(t time.Time) string {
	return t.Format("2006-01-02")
}
