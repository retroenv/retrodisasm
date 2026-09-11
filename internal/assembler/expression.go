package assembler

// FormatExpression renders a validated byte selector for the chosen assembler.
func FormatExpression(assembler, expression string) string {
	if len(expression) == 0 || expression[0] != '<' && expression[0] != '>' {
		return expression
	}
	value := "(" + expression[1:] + ")"
	if assembler == Nesasm {
		if expression[0] == '<' {
			return "LOW" + value
		}
		return "HIGH" + value
	}
	return expression[:1] + value
}
