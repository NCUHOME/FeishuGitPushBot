package bot

import (
	"testing"
)

func TestContainsMarkdownList(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		// ✅ 正常无序列表 - 行首
		{"dash at line start", "- item", true},
		{"star at line start", "* item", true},
		{"plus at line start", "+ item", true},
		// ✅ 正常无序列表 - 换行后
		{"dash after newline", "first line\n- item", true},
		{"star after newline", "first line\n* item", true},
		{"plus after newline", "first line\n+ item", true},
		// ✅ 缩进列表（2空格/4空格/制表符）
		{"indented 2 spaces at start", "  - item", true},
		{"indented 4 spaces at start", "    * item", true},
		{"indented with tab at start", "\t- item", true},
		{"indented 2 spaces after newline", "first line\n  - item", true},
		{"indented 4 spaces after newline", "first line\n    * item", true},
		{"indented with tab after newline", "first line\n\t- item", true},
		// ✅ 有序列表
		{"ordered 1. at line start", "1. item", true},
		{"ordered 2. at line start", "2. item", true},
		{"ordered 10. at line start", "10. item", true},
		{"ordered 100. at line start", "100. item", true},
		{"ordered after newline", "first line\n1. item", true},
		{"ordered indented", "  1. item", true},
		// ❌ 不完整的数字加点和数字后没空格（不应误判）
		{"number dot no space", "1.中文", false},
		{"number dot letter", "1.x", false},
		// ❌ 加减法/数学表达式（不应误判）
		{"negative number", "温度降至-5度", false},
		{"positive number", "今天+5度", false},
		{"subtraction inline", "a - b = c", false},
		{"addition inline", "a + b = c", false},
		{"dash as hyphen in word", "well-known", false},
		// ❌ markdown 加粗/斜体（不应误判）
		{"bold text", "这是**重要**内容", false},
		{"italic text", "这是*斜体*内容", false},
		{"bold with stars at line start", "**title**\ncontent", false},
		// ❌ 普通文本
		{"plain text", "只是一行普通文本", false},
		{"multiple lines without list", "line1\nline2\nline3", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsMarkdownList(tt.input)
			if got != tt.want {
				t.Errorf("containsMarkdownList(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
