// Command inspect 把一张表的解析过程摊开给人看。
//
// 线上排查"这张表为什么解析错了"时，先跑这个：
//
//	go run ./cmd/inspect -file testdata/raw/shop06-2024-06-006.xlsx
//
// 它会告诉你表头定位在第几行、每列被认成什么类型和字段。
// 比在日志里猜快得多。
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/jxu-hub/invoice-parser/internal/parser"
	"github.com/jxu-hub/invoice-parser/internal/sheet"
)

func main() {
	file := flag.String("file", "", "要检查的 xlsx 文件")
	show := flag.Int("n", 5, "打印前多少行数据")
	flag.Parse()

	if *file == "" {
		fmt.Fprintln(os.Stderr, "用法: inspect -file <xlsx>")
		os.Exit(2)
	}
	if err := run(*file, *show); err != nil {
		fmt.Fprintf(os.Stderr, "inspect: %v\n", err)
		os.Exit(1)
	}
}

func run(path string, show int) error {
	tbl, err := sheet.Load(path)
	if err != nil {
		return err
	}
	fmt.Printf("file       %s\n", path)
	fmt.Printf("rows       %d\n", len(tbl.Rows))
	fmt.Printf("columns    %d\n", tbl.Width())
	if tbl.Width() > 0 {
		fmt.Printf("row 0      %s\n", strings.Join(tbl.Rows[0], " | "))
	}

	res, err := parser.Parse(tbl)
	if err != nil {
		fmt.Printf("parse      FAILED: %v\n", err)
		fmt.Println("\nheader candidates")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "row\tscore\tnon-empty")
		for r := 0; r < len(tbl.Rows) && r < 8; r++ {
			fmt.Fprintf(w, "%d\t%.3f\t%d\n", r+1, parser.SplitScore(tbl, r), tbl.NonEmpty(r))
		}
		return w.Flush()
	}

	fmt.Printf("header row %d (score %.3f)\n", res.HeaderRow+1, res.Score)
	fmt.Printf("data rows  %d\n", len(res.Rows))

	fmt.Println("\ncolumns")
	for _, c := range res.Schema.Columns {
		src := "kind 兜底"
		if c.Matched {
			src = "列名别名"
		}
		field := string(c.Field)
		if field == "" {
			field = "-"
		}
		fmt.Printf("  %-2s  %-8s  %-5s  conf=%.2f  field=%-10s (%s)\n",
			colLetter(c.Index), c.Name, c.Kind, c.Confidence, field, src)
	}

	if len(res.Rows) > 0 && show > 0 {
		fmt.Println("\nparsed")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, strings.Join(parser.CSVHeader, "\t"))
		for i, r := range res.Rows {
			if i >= show {
				break
			}
			fmt.Fprintln(w, strings.Join(r.Fields(), "\t"))
		}
		return w.Flush()
	}
	return nil
}

func colLetter(i int) string {
	if i < 0 {
		return "?"
	}
	var s string
	for i >= 0 {
		s = string(rune('A'+i%26)) + s
		i = i/26 - 1
	}
	return s
}
