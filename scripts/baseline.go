// Command baseline 跑全量测试表，对比新旧两版实现的一次通过率。
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
	"github.com/jxu-hub/invoice-parser/internal/sheet"
	"github.com/jxu-hub/invoice-parser/internal/xlsx"
)

type outcome struct {
	entry golden.Sheet
	err   error
}

func main() {
	mfPath := flag.String("manifest", "testdata/manifest.json", "清单文件")
	show := flag.Int("show", 6, "每类最多显示多少条失败明细")
	flag.Parse()

	base := filepath.Dir(*mfPath)
	sheets, err := golden.Load(*mfPath)
	if err != nil {
		fatal(err)
	}

	oldOut := make([]outcome, 0, len(sheets))
	newOut := make([]outcome, 0, len(sheets))
	for _, s := range sheets {
		raw := filepath.Join(base, s.File)
		want := filepath.Join(base, s.Golden)
		oldOut = append(oldOut, outcome{entry: s, err: check(raw, want, runLegacy)})
		newOut = append(newOut, outcome{entry: s, err: check(raw, want, runCurrent)})
	}

	fmt.Print(render(sheets, oldOut, newOut, *show))
}

func check(rawPath, goldenPath string, run func(string) ([]parser.Row, error)) error {
	want, err := golden.Read(goldenPath)
	if err != nil {
		return err
	}
	got, err := run(rawPath)
	if err != nil {
		return err
	}
	return golden.Compare(want, got)
}

func runLegacy(path string) ([]parser.Row, error) {
	s, err := xlsx.Open(path)
	if err != nil {
		return nil, err
	}
	return parser.ParseLegacy(s.Rows)
}

func runCurrent(path string) ([]parser.Row, error) {
	t, err := sheet.Load(path)
	if err != nil {
		return nil, err
	}
	res, err := parser.Parse(t)
	if err != nil {
		return nil, err
	}
	return res.Rows, nil
}

func passes(os []outcome) int {
	n := 0
	for _, o := range os {
		if o.err == nil {
			n++
		}
	}
	return n
}

func render(sheets []golden.Sheet, oldOut, newOut []outcome, show int) string {
	var b strings.Builder
	total := len(sheets)

	fmt.Fprintf(&b, "invoice-parser baseline\nsheets: %d\n\n", total)
	fmt.Fprintf(&b, "%-14s %6s %6s %9s\n", "", "pass", "fail", "rate")
	fmt.Fprintf(&b, "%-14s %6d %6d %8.1f%%\n", "v1 (legacy)", passes(oldOut), total-passes(oldOut), rate(passes(oldOut), total))
	fmt.Fprintf(&b, "%-14s %6d %6d %8.1f%%\n", "v2 (current)", passes(newOut), total-passes(newOut), rate(passes(newOut), total))

	classes := classSet(sheets)
	fmt.Fprintf(&b, "\nby class\n%-10s %6s %9s %9s\n", "class", "total", "v1 fail", "v2 fail")
	for _, c := range classes {
		t, f1, f2 := classStats(sheets, oldOut, newOut, c)
		fmt.Fprintf(&b, "%-10s %6d %9d %9d\n", c, t, f1, f2)
	}

	for _, c := range classes {
		if lines := failuresOf(oldOut, newOut, c, show); len(lines) > 0 {
			fmt.Fprintf(&b, "\nv1 失败明细 [%s]\n", c)
			for _, l := range lines {
				fmt.Fprintf(&b, "  %s\n", l)
			}
		}
	}
	return b.String()
}

func rate(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(n) * 100 / float64(total)
}

func classSet(sheets []golden.Sheet) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range sheets {
		if !seen[s.Label] {
			seen[s.Label] = true
			out = append(out, s.Label)
		}
	}
	sort.Strings(out)
	return out
}

func classStats(sheets []golden.Sheet, oldOut, newOut []outcome, class string) (total, f1, f2 int) {
	for i, s := range sheets {
		if s.Label != class {
			continue
		}
		total++
		if oldOut[i].err != nil {
			f1++
		}
		if newOut[i].err != nil {
			f2++
		}
	}
	return total, f1, f2
}

func failuresOf(oldOut, newOut []outcome, class string, limit int) []string {
	var out []string
	matched := 0
	for i, o := range oldOut {
		if o.err == nil || o.entry.Label != class {
			continue
		}
		matched++
		if len(out) >= limit {
			continue
		}
		msg := oneLine(o.err.Error())
		if i < len(newOut) && newOut[i].err == nil {
			msg += "   [v2 已修复]"
		}
		out = append(out, fmt.Sprintf("%-34s %s", o.entry.File, msg))
	}
	if matched > limit {
		out = append(out, fmt.Sprintf("... 还有 %d 条", matched-limit))
	}
	return out
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", " ")), " ")
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "baseline: %v\n", err)
	os.Exit(1)
}
