// Command baseline 跑全量测试表，统计整表一次通过率。
//
// 这个脚本是整个仓库的地基：任何一次改动都要能回答"成功率变了没有、
// 把哪些表弄挂了"。没有它，改解析器就是凭手感。
//
//	go run ./scripts/baseline.go -manifest testdata/manifest.json
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jxu-hub/invoice-parser/internal/golden"
	"github.com/jxu-hub/invoice-parser/internal/parser"
	"github.com/jxu-hub/invoice-parser/internal/xlsx"
)

func main() {
	mfPath := flag.String("manifest", "testdata/manifest.json", "清单文件")
	show := flag.Int("show", 8, "每类最多显示多少条失败明细")
	flag.Parse()

	base := filepath.Dir(*mfPath)
	sheets, err := golden.Load(*mfPath)
	if err != nil {
		fatal(err)
	}

	failed := map[string][]string{}
	pass := 0
	for _, s := range sheets {
		want, err := golden.Read(filepath.Join(base, s.Golden))
		if err != nil {
			fatal(err)
		}
		src, err := xlsx.Open(filepath.Join(base, s.File))
		if err != nil {
			fatal(err)
		}
		rows, err := parser.ParseLegacy(src.Rows)
		if err == nil {
			err = golden.Compare(want, rows)
		}
		if err != nil {
			failed[s.Label] = append(failed[s.Label], fmt.Sprintf("%-34s %s", s.File, oneLine(err.Error())))
			continue
		}
		pass++
	}

	var b strings.Builder
	fmt.Fprintf(&b, "invoice-parser baseline\nsheets: %d\n\n", len(sheets))
	fmt.Fprintf(&b, "%-14s %6s %6s %9s\n", "", "pass", "fail", "rate")
	fmt.Fprintf(&b, "%-14s %6d %6d %8.1f%%\n", "current", pass, len(sheets)-pass,
		float64(pass)*100/float64(len(sheets)))

	classes := map[string]bool{}
	for _, s := range sheets {
		classes[s.Label] = true
	}
	names := make([]string, 0, len(classes))
	for c := range classes {
		names = append(names, c)
	}
	sort.Strings(names)

	fmt.Fprintf(&b, "\nby class\n%-10s %6s %9s\n", "class", "total", "fail")
	for _, c := range names {
		total := 0
		for _, s := range sheets {
			if s.Label == c {
				total++
			}
		}
		fmt.Fprintf(&b, "%-10s %6d %9d\n", c, total, len(failed[c]))
	}

	for _, c := range names {
		if len(failed[c]) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n失败明细 [%s]\n", c)
		for i, line := range failed[c] {
			if i >= *show {
				fmt.Fprintf(&b, "  ... 还有 %d 条\n", len(failed[c])-*show)
				break
			}
			fmt.Fprintf(&b, "  %s\n", line)
		}
	}
	fmt.Print(b.String())
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", " ")), " ")
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "baseline: %v\n", err)
	os.Exit(1)
}
