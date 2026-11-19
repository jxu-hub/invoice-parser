# invoice-parser

[![ci](https://github.com/jxu-hub/invoice-parser/actions/workflows/ci.yml/badge.svg)](https://github.com/jxu-hub/invoice-parser/actions/workflows/ci.yml)

把各家电商系统导出的"脏"Excel 销售明细，解析成干净、可比对、可进库的 CSV。

不是万能表格识别器，只做一件事：**订单明细表**。为这一件事把该做的做扎实。

---

## 为什么会有这个项目

同一批订单，六个店长导出六种表：表头上面压着公司抬头，金额带 `¥` 和千分位，
日期有五种写法，合并单元格把"金额与数量"盖在单价/数量/小计上面，
表尾还有一行"合计"和"第 1 页 / 共 1 页"。

原来的解析器只有三十行：第一行当表头、按列名精确匹配、每个值严格
`ParseFloat` / `ParseTime`，任何一处对不上，**整张表失败**。

结果就是：200 张表里 63 张直接丢掉，而丢掉的那些表里 94% 的行本来是好数据。

## 结果

在 200 张测试表上（`testdata/`，合成数据，见下文）：

| 版本 | 一次通过 | 失败 | 通过率 |
|---|---|---|---|
| v1（旧实现，仍保留在仓库里） | 137 | 63 | 68.5% |
| v2（当前实现） | 200 | 0 | 100% |

按失败类型拆开：

| 类型 | 表数 | v1 失败 | v2 失败 |
|---|---|---|---|
| clean（干净的） | 137 | 0 | 0 |
| header（表头上面有抬头/空行） | 21 | 21 | 0 |
| merged（两行表头 + 合并单元格） | 18 | 18 | 0 |
| money（金额带 ¥ / 元 / 千分位 / 括号负数） | 14 | 14 | 0 |
| date（日期五种写法） | 7 | 7 | 0 |
| other（合计行 / 重复表头 / 脚注） | 3 | 3 | 0 |

详细报告见 [RESULTS.md](RESULTS.md)，`go run ./scripts/baseline.go` 可随时复算。

## 四个设计决定

**1. 表头靠打分找，不靠"第一行"**

`detectHeader` 在表头行里扫前 20 行，先滤掉标题行（非空格子太少）、
组表头（有跨列合并），再按 `SplitScore` 打分：

- 下面各列的类型一致性（越高越好）
- 表头自己有多少格长得像数字/日期/单号（越少越好）
- 列名命中别名表的比例（越高越好）

只算第一项分不出表头和数据行——任意一行数据当下，它下面的列同样整齐。
另外两项才是关键。

**2. 列类型推断用来兜底，也用来定表头**

`inferKind` 每列采样 200 行投票，返回多数类型和一致性。
列名匹配不上别名表时，按类型和数量关系兜底认列；数量对不上就**不猜**——
猜错一列比缺一列代价大得多。

单号判定要求"有前导零或足够长"，否则 `000123` 会被当数字，前导零全丢。
日期判定要求年份落在 1900–2100，否则 `00001101` 这种 8 位单号会被
`20060102` 布局吃成公元 0 年。

**3. 单行失败不再拖垮整表**

解析失败的行进隔离区，带行号和原因，其余行照常输出。
生产上平均 6% 的行需要人工或模型复核，为一个行丢掉 94% 是不划算的。

**4. 回归集是地基**

`go test ./...` 跑 200 张表的逐字段比对，任何一张表的任何一格变了都会红。
`TestNoRegressionAgainstLegacy` 额外保证：v1 能过的表，v2 必须也能过。

再加一层防误判：v1 的基线数字（137/200、每类失败数）写死在
`legacy_test.go` 里。有人哪天"顺手优化"了旧实现，测试立刻炸。

## 跑起来

```bash
go test ./...                                   # 200 张表的完整回归
go run ./scripts/baseline.go                    # v1 vs v2 通过率对比
go run ./scripts/baseline.go -md RESULTS.md     # 重新生成结果报告
go run ./cmd/inspect -file testdata/raw/shop06-2024-06-006.xlsx   # 看一张表怎么被解析
```

跑批（幂等、带结构化日志、失败重试）：

```bash
mkdir -p inbox && cp testdata/raw/*.xlsx inbox/
go run ./cmd/collector -once -dir inbox -out parsed
```

日志一行一个 JSON，带 `batch_id / file / md5 / rows / isolated / duration_ms`。
同一文件按 md5 去重，重跑不会写出重复数据。

## 目录

```
cmd/collector        跑批入口：md5 幂等、指数退避重试、slog JSON 日志
cmd/inspect          单表解剖：表头候选打分、每列的类型/字段/来源
cmd/genfixtures      生成 200 张合成测试表 + golden 答案
internal/xlsx        最小 xlsx 读取器（纯标准库，含合并单元格）
internal/sheet       展平合并单元格、去噪
internal/parser      kind 推断 / 表头打分 / 金额日期归一化 / schema 对比 / 隔离区
internal/llm         隔离行复核（OpenAI 兼容，未配置则不启用）
internal/golden      golden 答案读取与比较（脚本和测试共用）
scripts/baseline     全量跑批对比
```

## 关于 testdata

**仓库里的 200 张表全部是 `cmd/genfixtures` 合成出来的**，不含任何真实客户数据。
这不是"顺手造几个例子"，而是按真实表格的毛病分类生成的：每张都是真正的
xlsx（zip + OOXML），覆盖 sharedStrings 和 inlineStr 两种编码、合并单元格、
五种日期写法和四种金额写法。

`testdata/labels.csv` 记录每张表属于哪一类失败，baseline 报告按它分组。

重跑生成器会改变 fixture 内容，进而改变基线数字——**除非你打算重建基线，
否则不要重新生成**。

## 已知边界

100% 是这 200 张合成表上的 100%，不等于对任意表格 100%。目前明确不处理：

- 多个工作表、跨 sheet 引用、`sheet1.xml` 以外的数据
- 公式求值（读到的是公式缓存值，没缓存就是空）
- 三层以上的嵌套表头（代码只跳一层组表头）
- 没有年份的日期（"1月5日"）、"昨天/前天"这类相对日期
- 纵向合并的数据行（跨行合并只展平值，不做行合并）
- 金额与数量对不上时的交叉校验（目前只做解析，不做业务校验）

这些不是遗漏，是取舍：每加一条都要在回归集上有对应的一组表，否则就是给自己加盲区。

## 复用

`internal/xlsx` 和 `internal/sheet` 与业务无关，可以直接拿去用。
解析器的表结构假设集中在 `internal/parser/schema.go` 的别名表里，
换业务（比如改成采购单、账单）改那里就行。

## License

MIT，见 [LICENSE](LICENSE)。
