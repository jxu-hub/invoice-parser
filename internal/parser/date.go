package parser

import (
	"fmt"
	"strings"
	"time"
)

// ParseDate 解析日期。
// 目前只认 ISO 格式：导出工具里但凡出现别的写法，就整表失败——
// 先知道失败在哪，再决定要不要放宽。
func ParseDate(raw string) (time.Time, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, fmt.Errorf("日期为空")
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("日期 %q 无法解析", raw)
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), nil
}

// FormatDate 是写入 golden / 输出时的唯一日期格式。
func FormatDate(t time.Time) string {
	return t.Format("2006-01-02")
}

// IsDate 判断字符串能否被解析成日期。
func IsDate(s string) bool {
	_, err := ParseDate(s)
	return err == nil
}
