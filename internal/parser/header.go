package parser

import (
	"github.com/jxu-hub/invoice-parser/internal/sheet"
)

const (
	// maxHeaderScan 只扫前 20 行找表头。再往下的"表头"基本都是数据里重复出现的行。
	maxHeaderScan = 20
	// minHeaderCells 是成为表头候选的最低非空单元格数，用于剔除标题行、备注行。
	minHeaderCells = 3
	// sampleRows 是推断列类型时向下采样的行数上限。
	sampleRows = 200
)

// DetectHeader 找真表头所在的行。
//
// 做法是先筛候选，再打分：非空单元格太少的是标题行/制表人行/空行，
// 剩下的行按 SplitScore 排序，分数最低的就是表头。
//
// 为什么不能只看"下面数据一不一致"：任意一行数据行作为表头，
// 它下面的列类型同样很一致，真正的区分点是表头自己不能长得像数据。
func DetectHeader(t *sheet.Table) (row int, score float64, ok bool) {
	lastCandidate := len(t.Rows) - 1
	bestRow, bestScore := -1, 0.0

	limit := len(t.Rows)
	if limit > maxHeaderScan {
		limit = maxHeaderScan
	}
	for r := 0; r < limit; r++ {
		if r >= lastCandidate { // 至少要留一行数据
			break
		}
		if t.NonEmpty(r) < minHeaderCells {
			continue
		}
		s := SplitScore(t, r)
		if bestRow < 0 || s < bestScore {
			bestRow, bestScore = r, s
		}
	}
	if bestRow < 0 {
		return 0, 0, false
	}
	return bestRow, bestScore, true
}

// SplitScore 是"这一行像不像表头"的分裂度，越小越像。三个分量：
//
//	typeSplit   下面各列的类型一致性，越一致越好（权重最高）
//	numericCell 表头自身有多少格像数字/日期/单号，越少越好
//	aliasMiss   列名有多少不在别名表里，越少越好
//
// 单看第一项分不出表头和数据行，后两项才是关键。
func SplitScore(t *sheet.Table, r int) float64 {
	width := t.Width()

	typeSplit, typeCols := 0.0, 0
	nonEmptyCells, numericCells, aliasHits := 0, 0, 0

	for c := 0; c < width; c++ {
		name := t.Rows[r][c]
		if name == "" {
			typeSplit += 1.0
			typeCols++
			continue
		}
		nonEmptyCells++
		k := Classify(name)
		if k == KindMoney || k == KindDate || k == KindID {
			numericCells++
		}
		if _, ok := MapHeader(name); ok {
			aliasHits++
		}
		if vals := columnValues(t, r, c); len(vals) >= 3 {
			if _, consistency := InferKind(vals); consistency > 0 {
				typeSplit += 1 - consistency
			} else {
				typeSplit += 1.0
			}
			typeCols++
		}
	}

	split := 1.0
	if typeCols > 0 {
		split = typeSplit / float64(typeCols)
	}
	numericPenalty := 1.0
	aliasMiss := 1.0
	if nonEmptyCells > 0 {
		numericPenalty = float64(numericCells) / float64(nonEmptyCells)
		aliasMiss = 1 - float64(aliasHits)/float64(nonEmptyCells)
	}
	return 0.5*split + 0.3*numericPenalty + 0.2*aliasMiss
}

// columnValues 采表头下面一列的值。
func columnValues(t *sheet.Table, headerRow, col int) []string {
	var vals []string
	for r := headerRow + 1; r < len(t.Rows) && len(vals) < sampleRows; r++ {
		if col >= len(t.Rows[r]) {
			continue
		}
		if v := t.Rows[r][col]; v != "" {
			vals = append(vals, v)
		}
	}
	return vals
}
