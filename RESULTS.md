# 结果

数据来源：`testdata/` 下 200 张表，`go run ./scripts/baseline.go` 输出。
判定标准是**整表一次过**：行数一致，且每一行七个字段全部一致；
少一行、多一行、差一个小数点，这张表就算失败。

## 汇总

| 版本 | 通过 | 失败 | 通过率 |
|---|---|---|---|
| v1（旧实现） | 137 | 63 | 68.5% |
| v2（当前） | 200 | 0 | 100% |

## 分类

| 类型 | 表数 | v1 失败 | v2 失败 | 主要失败原因 |
|---|---|---|---|---|
| clean | 137 | 0 | 0 | — |
| header | 21 | 21 | 0 | 表头上面有公司抬头/空行/制表人行，v1 把第一行当表头 |
| merged | 18 | 18 | 0 | 两行表头 + 合并单元格，v1 读到的是组表头，且合并区域读成空格 |
| money | 14 | 14 | 0 | `¥1,234.50` / `1,234.5元` / `(120.00)`，v1 直接 `ParseFloat` |
| date | 7 | 7 | 0 | `2024/1/5`、`2024年1月5日`、`20240105`、`05-Jan-24` |
| other | 3 | 3 | 0 | 表尾合计行、中途重复表头、脚注页码 |

63 = 21 + 18 + 14 + 7 + 3，和 v1 的失败数完全对上。

## 原始输出

```
invoice-parser baseline
sheets: 200

                 pass   fail      rate
v1 (legacy)       137     63     68.5%
v2 (current)      200      0    100.0%

by class
class       total   v1 fail   v2 fail
clean         137         0         0
date            7         7         0
header         21        21         0
merged         18        18         0
money          14        14         0
other           3         3         0

v1 失败明细 [date]
  raw/shop02-2024-02-002.xlsx        legacy: 第 2 行: 日期 "2024-1-2"   [v2 已修复]
  raw/shop03-2024-11-035.xlsx        legacy: 第 2 行: 日期 "2024年01月03日"   [v2 已修复]
  raw/shop02-2024-10-058.xlsx        legacy: 第 2 行: 日期 "03-Jan-24"   [v2 已修复]
  ... 还有 4 条

v1 失败明细 [header]
  raw/shop05-2024-09-021.xlsx        legacy: 表头缺少字段 order_id   [v2 已修复]
  raw/shop04-2024-04-028.xlsx        legacy: 表头缺少字段 order_id   [v2 已修复]
  raw/shop05-2024-05-029.xlsx        legacy: 表头缺少字段 order_id   [v2 已修复]
  ... 还有 18 条

v1 失败明细 [merged]
  raw/shop06-2024-06-006.xlsx        legacy: 表头缺少字段 order_id   [v2 已修复]
  raw/shop08-2024-08-008.xlsx        legacy: 表头缺少字段 order_id   [v2 已修复]
  raw/shop03-2024-11-011.xlsx        legacy: 表头缺少字段 order_id   [v2 已修复]
  ... 还有 15 条

v1 失败明细 [money]
  raw/shop08-2024-12-024.xlsx        legacy: 第 2 行: 单价 "15.80元"   [v2 已修复]
  raw/shop04-2024-08-044.xlsx        legacy: 第 2 行: 单价 "¥3,200.00"   [v2 已修复]
  raw/shop01-2024-01-049.xlsx        legacy: 第 2 行: 单价 "88.00元"   [v2 已修复]
  ... 还有 11 条

v1 失败明细 [other]
  raw/shop07-2024-11-071.xlsx        legacy: 第 13 行: 日期 ""   [v2 已修复]
  raw/shop05-2024-01-181.xlsx        legacy: 第 13 行: 日期 ""   [v2 已修复]
  raw/shop03-2024-03-195.xlsx        legacy: 第 10 行: 日期 ""   [v2 已修复]
```

## 怎么复现

```bash
go test ./...                                   # 200 张表逐字段比对
go run ./scripts/baseline.go                    # 上面这份报告
go run ./scripts/baseline.go -md RESULTS.md     # 重新生成本文件
```

## 这个 100% 不代表什么

- fixture 是合成的（`cmd/genfixtures`），覆盖的是"已知的五类毛病"，
  不是互联网上所有表格。真实表格里一定还有第六类、第七类。
- 解析正确 ≠ 业务正确。金额和数量是否对得上，这里不做校验。
- 层数更深的表头、多工作表、公式求值都没做，见 README 的"已知边界"。

真实的验收方式是：拿自己的表跑 `go run ./cmd/inspect -file xxx.xlsx`，
把不认识的那些表补成新的 fixture 类别，再让通过率重新爬回来。
