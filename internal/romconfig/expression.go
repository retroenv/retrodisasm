package romconfig

import (
	"fmt"
	"regexp"
	"strings"
)

var expressionToken = regexp.MustCompile(`^(?:[A-Za-z_][A-Za-z_0-9]*|\$[0-9a-fA-F]+|%[01]+|0[xX][0-9a-fA-F]+|[0-9]+)`)

// Evaluate resolves a restricted assembler expression without executing directives.
// It accepts names, integers, addition/subtraction, and a leading low/high-byte selector.
func Evaluate(expression string, symbols map[string]uint16) (uint16, error) {
	expression = strings.ReplaceAll(strings.TrimSpace(expression), " ", "")
	var selector byte
	if len(expression) > 0 && (expression[0] == '<' || expression[0] == '>') {
		selector, expression = expression[0], expression[1:]
	}

	value, err := evaluateSum(expression, symbols)
	if err != nil {
		return 0, err
	}
	if value < 0 || value > 0xffff {
		return 0, fmt.Errorf("expression value %d is outside 16-bit range", value)
	}
	switch selector {
	case '<':
		value &= 0xff
	case '>':
		value >>= 8
	}
	return uint16(value), nil
}

func evaluateSum(expression string, symbols map[string]uint16) (int, error) {
	value, sign := 0, 1
	for {
		token := expressionToken.FindString(expression)
		if token == "" {
			return 0, fmt.Errorf("invalid operand expression %q", expression)
		}
		number, ok := symbols[token]
		if !ok {
			var err error
			number, err = parseAddress(token)
			if err != nil {
				return 0, fmt.Errorf("unknown operand symbol %q", token)
			}
		}

		value += sign * int(number)
		expression = expression[len(token):]
		if expression == "" {
			return value, nil
		}

		switch expression[0] {
		case '+':
			sign = 1
		case '-':
			sign = -1
		default:
			return 0, fmt.Errorf("invalid operand operator %q", expression)
		}
		expression = expression[1:]
	}
}
