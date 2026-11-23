package billingexpr

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/expr-lang/expr/file"
	"github.com/expr-lang/expr/parser/lexer"
)

// normalizeLegacyRules accepts the documented base|||when(condition) * rate
// syntax. Lexing first keeps delimiters and "has" inside strings literal.
func normalizeLegacyRules(body string) (string, error) {
	if !strings.Contains(body, "|||") {
		return body, nil
	}
	tokens, err := lexer.Lex(file.NewSource(body))
	if err != nil {
		return "", err
	}
	src := []rune(body)
	start := 0
	var parts []string
	for i := 0; i+1 < len(tokens); i++ {
		a, b := tokens[i], tokens[i+1]
		if a.Is(lexer.Operator, "||") && b.Is(lexer.Operator, "|") && a.To == b.From {
			parts = append(parts, string(src[start:a.From]))
			start = b.To
		}
	}
	if len(parts) == 0 {
		return body, nil
	}
	parts = append(parts, string(src[start:]))
	base := "(" + parts[0] + ")"
	for _, part := range parts[1:] {
		ts, err := lexer.Lex(file.NewSource(part))
		if err != nil {
			return "", err
		}
		if len(ts) > 0 && ts[len(ts)-1].Kind == lexer.EOF {
			ts = ts[:len(ts)-1]
		}
		n := len(ts)
		if n < 6 || !ts[0].Is(lexer.Identifier, "when") || !ts[1].Is(lexer.Bracket, "(") || !ts[n-3].Is(lexer.Bracket, ")") || !ts[n-2].Is(lexer.Operator, "*") || ts[n-1].Kind != lexer.Number {
			return "", fmt.Errorf("expected when(condition) * multiplier")
		}
		rate, err := strconv.ParseFloat(ts[n-1].Value, 64)
		if err != nil || rate < 0 || math.IsInf(rate, 0) {
			return "", fmt.Errorf("invalid request multiplier")
		}
		runes := []rune(part)
		var condition strings.Builder
		pos := ts[1].To
		for i := 2; i < n-3; i++ {
			if ts[i].Is(lexer.Identifier, "has") && !ts[i+1].Is(lexer.Bracket, "(") {
				condition.WriteString(string(runes[pos:ts[i].From]))
				condition.WriteString(" contains ")
				pos = ts[i].To
			}
		}
		condition.WriteString(string(runes[pos:ts[n-3].From]))
		base += " * ((" + condition.String() + ") ? " + ts[n-1].Value + " : 1)"
	}
	return base, nil
}
