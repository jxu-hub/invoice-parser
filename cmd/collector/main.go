// Command collector 是定时跑批的入口。
//
// 一次跑批要满足三件事，缺一件就会在生产上变成事故：
//
//	幂等    同一个文件按 md5 去重，重跑不会写出重复数据
//	可定位  每条日志带 batch_id / 文件名 / 耗时 / 失败原因，出问题能直接查
//	不静默  单文件失败会重试并记录，隔离行单独落盘等人工复核
package main

import (
	"context"
	"crypto/md5"
	"encoding/csv"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jxu-hub/invoice-parser/internal/llm"
	"github.com/jxu-hub/invoice-parser/internal/parser"
	"github.com/jxu-hub/invoice-parser/internal/sheet"
)

func main() {
	inbox := flag.String("dir", "inbox", "待处理 xlsx 目录")
	outDir := flag.String("out", "parsed", "解析结果输出目录")
	stateDir := flag.String("state", ".collector", "状态目录（md5 去重记录）")
	once := flag.Bool("once", false, "只跑一轮就退出")
	interval := flag.Duration("interval", 5*time.Minute, "轮询间隔")
	retries := flag.Int("retries", 3, "单文件重试次数")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	c := &collector{
		logger:  logger,
		repair:  llm.NewFromEnv(),
		outDir:  *outDir,
		retries: *retries,
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		logger.Error("创建输出目录失败", "err", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(*stateDir, 0o755); err != nil {
		logger.Error("创建状态目录失败", "err", err)
		os.Exit(1)
	}

	batchID := time.Now().Format("20060102T150405")
	c.loadState(filepath.Join(*stateDir, "processed.txt"))

	for {
		n, err := c.runOnce(context.Background(), batchID, *inbox, filepath.Join(*stateDir, "processed.txt"))
		if err != nil {
			logger.Error("跑批失败", "batch_id", batchID, "err", err)
		} else {
			logger.Info("跑批完成", "batch_id", batchID, "files", n)
		}
		if *once {
			return
		}
		time.Sleep(*interval)
	}
}

type collector struct {
	logger    *slog.Logger
	repair    llm.Repairer
	outDir    string
	retries   int
	processed map[string]bool
	donePath  string
}

func (c *collector) loadState(path string) {
	c.donePath = path
	c.processed = map[string]bool{}
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(b), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			c.processed[line] = true
		}
	}
}

func (c *collector) runOnce(ctx context.Context, batchID, inbox, statePath string) (int, error) {
	entries, err := os.ReadDir(inbox)
	if err != nil {
		return 0, fmt.Errorf("读取目录 %s: %w", inbox, err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".xlsx") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names) // 固定顺序，失败重跑时的日志才好对比

	ok := 0
	for _, name := range names {
		path := filepath.Join(inbox, name)
		if err := c.processFile(ctx, batchID, path, statePath); err != nil {
			c.logger.Error("文件处理失败", "batch_id", batchID, "file", name, "err", err)
			continue
		}
		ok++
	}
	return ok, nil
}

type stats struct {
	path     string
	md5      string
	rows     int
	isolated int
	skipped  int
	repaired int
}

func (c *collector) processFile(ctx context.Context, batchID, path, statePath string) error {
	sum, err := fileMD5(path)
	if err != nil {
		return err
	}
	name := filepath.Base(path)
	if c.processed[sum] {
		c.logger.Info("跳过已处理文件", "batch_id", batchID, "file", name, "md5", sum)
		return nil
	}

	var st stats
	var lastErr error
	for attempt := 1; attempt <= c.retries; attempt++ {
		start := time.Now()
		st, lastErr = c.processOne(ctx, path, sum)
		dur := time.Since(start).Milliseconds()

		if lastErr == nil {
			c.logger.Info("解析成功",
				"batch_id", batchID, "file", name, "md5", sum,
				"rows", st.rows, "isolated", st.isolated,
				"skipped", st.skipped, "repaired", st.repaired,
				"attempt", attempt, "duration_ms", dur)
			c.markDone(statePath, sum)
			return nil
		}

		c.logger.Warn("解析失败，准备重试",
			"batch_id", batchID, "file", name, "attempt", attempt,
			"max_attempts", c.retries, "duration_ms", dur, "err", lastErr)

		// 指数退避，避免上游临时不可用时把 CPU 打满
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(300*(1<<uint(attempt-1))) * time.Millisecond):
		}
	}
	return fmt.Errorf("重试 %d 次仍失败: %w", c.retries, lastErr)
}

func (c *collector) processOne(ctx context.Context, path, sum string) (stats, error) {
	st := stats{path: path, md5: sum}

	tbl, err := sheet.Load(path)
	if err != nil {
		return st, err
	}
	res, err := parser.Parse(tbl)
	if err != nil {
		return st, err
	}
	st.rows, st.isolated, st.skipped = len(res.Rows), len(res.Isolated), res.Skipped

	// 隔离行先交给复核（没配模型时是 Noop，什么也不做）
	if st.isolated > 0 {
		fixed, err := c.repair.Repair(ctx, llm.Request{
			HeaderRow: res.HeaderRow,
			Header:    tbl.Rows[res.HeaderRow],
			Rows:      res.Isolated,
		})
		if err != nil {
			c.logger.Warn("隔离行复核失败，按原样隔离", "file", filepath.Base(path), "err", err)
		}
		if len(fixed) > 0 {
			res.Rows = append(res.Rows, fixed...)
			st.repaired = len(fixed)
			st.rows = len(res.Rows)
		}
	}

	if err := c.writeCSV(path, res.Rows); err != nil {
		return st, err
	}
	if len(res.Isolated) > 0 {
		if err := c.writeIsolated(path, res.Isolated); err != nil {
			return st, err
		}
	}
	return st, nil
}

func (c *collector) writeCSV(srcPath string, rows []parser.Row) error {
	base := strings.TrimSuffix(filepath.Base(srcPath), filepath.Ext(srcPath))
	path := filepath.Join(c.outDir, base+".csv")
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	if err := w.Write(parser.CSVHeader); err != nil {
		return err
	}
	for _, r := range rows {
		if err := w.Write(r.Fields()); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func (c *collector) writeIsolated(srcPath string, rows []parser.IsolatedRow) error {
	base := strings.TrimSuffix(filepath.Base(srcPath), filepath.Ext(srcPath))
	path := filepath.Join(c.outDir, base+".isolated.csv")
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	if err := w.Write([]string{"line", "reason", "raw"}); err != nil {
		return err
	}
	for _, r := range rows {
		if err := w.Write([]string{
			fmt.Sprintf("%d", r.Line),
			r.Reason,
			strings.Join(r.Raw, " | "),
		}); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func (c *collector) markDone(statePath, sum string) {
	c.processed[sum] = true
	f, err := os.OpenFile(statePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		c.logger.Error("写状态文件失败，下轮可能重复处理", "err", err, "md5", sum)
		return
	}
	defer f.Close()
	if _, err := fmt.Fprintln(f, sum); err != nil {
		c.logger.Error("写状态文件失败", "err", err)
	}
}

func fileMD5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
