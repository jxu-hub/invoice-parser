// Package llm 只做一件事：把解析器隔离出来的行，交给大模型复核。
//
// 设计上刻意做得很窄——模型只拿隔离行，不拿整张表；
// 它只被允许输出 JSON，输出必须过 schema 校验，不合法就重试一次。
// 模型在这里是"兜底"，不是主路径：主路径永远是规则解析。
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jxu-hub/invoice-parser/internal/parser"
)

// Request 是一次复核请求：要复核哪些行，这些行的原始单元格是什么。
type Request struct {
	HeaderRow int
	Header    []string
	Rows      []parser.IsolatedRow
}

// Repairer 是把隔离行补回来的能力。没配模型时用 Noop。
type Repairer interface {
	Repair(ctx context.Context, req Request) ([]parser.Row, error)
}

// Noop 什么都不做：隔离行保持隔离，交给人工。
type Noop struct{}

// Repair 实现 Repairer。
func (Noop) Repair(context.Context, Request) ([]parser.Row, error) { return nil, nil }

// NewFromEnv 按环境变量决定用哪个实现：
//
//	REPAIR_BASE_URL  形如 https://api.example.com/v1
//	REPAIR_MODEL     模型名
//	REPAIR_API_KEY   密钥
//
// 三个都没配就返回 Noop —— 这个项目不要求你有模型也能跑。
func NewFromEnv() Repairer {
	base := os.Getenv("REPAIR_BASE_URL")
	model := os.Getenv("REPAIR_MODEL")
	key := os.Getenv("REPAIR_API_KEY")
	if base == "" || model == "" || key == "" {
		return Noop{}
	}
	return &OpenAI{
		BaseURL: strings.TrimSuffix(base, "/"),
		Model:   model,
		APIKey:  key,
		Client:  &http.Client{Timeout: 60 * time.Second},
	}
}

// OpenAI 是 OpenAI 兼容的 chat/completions 客户端。
type OpenAI struct {
	BaseURL string
	Model   string
	APIKey  string
	Client  *http.Client
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

// maxAttempts 是重试次数。第一次输出不合法时再问一次，
// 第二次还不合法就放弃——不要在这上面无限烧钱。
const maxAttempts = 2

// Repair 把隔离行交给模型，返回能通过校验的那些行。
func (o *OpenAI) Repair(ctx context.Context, req Request) ([]parser.Row, error) {
	if len(req.Rows) == 0 {
		return nil, nil
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		raw, err := o.call(ctx, req, lastErr)
		if err != nil {
			return nil, err
		}
		rows, err := parseRows(raw)
		if err != nil {
			lastErr = err
			continue // 把上次的错误回传给模型，再试一次
		}
		return rows, nil
	}
	return nil, fmt.Errorf("llm: 复核结果校验失败: %w", lastErr)
}

func (o *OpenAI) call(ctx context.Context, req Request, prevErr error) (string, error) {
	system := `You repair broken rows from a Chinese sales-detail spreadsheet. ` +
		`You will get the header and the raw cells of rows that failed to parse. ` +
		`Return ONLY a JSON array, no prose, no code fence. Each element: ` +
		`{"order_id":string,"order_date":"YYYY-MM-DD","product":string,` +
		`"unit_price":number,"qty":integer,"amount":number,"note":string}. ` +
		`Never invent a value: if a cell is truly unusable, drop that row from the array.`

	payload, err := json.Marshal(map[string]any{
		"header": req.Header,
		"rows":   req.Rows,
	})
	if err != nil {
		return "", err
	}

	user := "header and failed rows:\n" + string(payload)
	if prevErr != nil {
		user += "\n\nYour previous answer was rejected: " + prevErr.Error() + "\nReturn corrected JSON only."
	}

	body, err := json.Marshal(chatRequest{
		Model: o.Model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: 0,
	})
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.APIKey)

	resp, err := o.Client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("llm: 请求失败: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("llm: HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}

	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "", fmt.Errorf("llm: 响应不是合法 JSON: %w", err)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("llm: 响应里没有 choices")
	}
	return cr.Choices[0].Message.Content, nil
}

// parseRows 解析并校验模型输出。
// 校验不放水：日期必须能解析、金额必须能解析，否则整批判定失败重试。
func parseRows(raw string) ([]parser.Row, error) {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)

	var items []struct {
		OrderID   string   `json:"order_id"`
		Date      string   `json:"order_date"`
		Product   string   `json:"product"`
		UnitPrice *float64 `json:"unit_price"`
		Qty       *int     `json:"qty"`
		Amount    *float64 `json:"amount"`
		Note      string   `json:"note"`
	}
	if err := json.Unmarshal([]byte(s), &items); err != nil {
		return nil, fmt.Errorf("不是 JSON 数组: %w", err)
	}

	out := make([]parser.Row, 0, len(items))
	for i, it := range items {
		if it.OrderID == "" || it.UnitPrice == nil || it.Qty == nil || it.Amount == nil {
			return nil, fmt.Errorf("第 %d 条缺字段", i+1)
		}
		d, err := parser.ParseDate(it.Date)
		if err != nil {
			return nil, fmt.Errorf("第 %d 条日期不合法: %w", i+1, err)
		}
		out = append(out, parser.Row{
			OrderID:   it.OrderID,
			Date:      parser.FormatDate(d),
			Product:   it.Product,
			UnitPrice: *it.UnitPrice,
			Qty:       *it.Qty,
			Amount:    *it.Amount,
			Note:      it.Note,
		})
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
