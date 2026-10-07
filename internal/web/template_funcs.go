package web

import "html/template"

// GetTemplateFuncs returns template functions for use in HTML templates
func GetTemplateFuncs() template.FuncMap {
	return template.FuncMap{
		"add":  func(a, b int) int { return a + b },
		"sub":  func(a, b int) int { return a - b },
		"mul":  func(a, b int) int { return a * b },
		"safe": func(s string) template.HTML { return template.HTML(s) },
		"seq": func(start, end int) []int {
			var result []int
			if start > end {
				return result
			}
			for i := start; i <= end; i++ {
				result = append(result, i)
			}
			return result
		},
		// "iterate" function for pagination - generates a slice of integers from start to end
		"iterate": func(start, end int) []int {
			var result []int
			if start > end {
				return result
			}
			for i := start; i <= end; i++ {
				result = append(result, i)
			}
			return result
		},
	}
}
