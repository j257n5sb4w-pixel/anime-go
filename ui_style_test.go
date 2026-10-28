package main

// 界面样式的回归测试（2026-10-28 补）。
//
// 起因：设置页的密码框明显比旁边的文本框又短又窄。根因是基础输入框样式
// 写成了 `input[type=text],input[type=number],...`，只匹配**显式写了**
// type 的输入框，于是
//   - `type="password"` 完全没被选中；
//   - 没写 type 的 <input>（默认即 text）也没被选中。
// 两类都退化成浏览器默认样式：宽度不撑满、上下留白更小。
//
// 这类问题 go vet 查不出来，单元测试也不会失败，只会在界面上「看起来不太对」。
// 所以这里用最朴素的办法锁住：解析 webUI 里的 CSS 选择器，确认覆盖了所有
// 会当文本框用的 input，并且给每个用到的 type 都配了样式。

import (
	"regexp"
	"strings"
	"testing"
)

// baseInputRule 从 webUI 里取出那条基础输入框规则的选择器部分。
func baseInputRule(t *testing.T) string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^(input\[type=[^\n{]*?,select,textarea)\{`)
	m := re.FindStringSubmatch(webUI)
	if m == nil {
		t.Fatal("找不到基础输入框样式规则（input[type=...],select,textarea{...}）")
	}
	return m[1]
}

func TestBaseInputStyleCoversPassword(t *testing.T) {
	rule := baseInputRule(t)
	if !strings.Contains(rule, "input[type=password]") {
		t.Errorf("基础输入框样式没有覆盖 password，密码框会走浏览器默认样式：\n  %s", rule)
	}
}

func TestBaseInputStyleCoversTypelessInput(t *testing.T) {
	rule := baseInputRule(t)
	if !strings.Contains(rule, "input:not([type])") {
		t.Errorf("基础输入框样式没有覆盖无 type 的 <input>（默认即 text）：\n  %s", rule)
	}
}

// 界面里实际用到的每个 input type，都必须在这条基础规则里出现，
// 否则新增一个 type 时会悄悄退回默认样式。
func TestEveryUsedInputTypeIsStyled(t *testing.T) {
	rule := baseInputRule(t)

	// 这些类型本来就不该走「文本框」样式：尺寸由各自的类控制。
	skip := map[string]bool{
		"checkbox": true,
		"radio":    true,
		"file":     true,
		"hidden":   true,
		"submit":   true,
		"button":   true,
		"range":    true,
		"color":    true,
		"date":     true, // 若将来用到再补样式
	}

	re := regexp.MustCompile(`<input[^>]*type="([a-zA-Z]+)"`)
	seen := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(webUI, -1) {
		typ := strings.ToLower(m[1])
		if skip[typ] || seen[typ] {
			continue
		}
		seen[typ] = true
		if !strings.Contains(rule, "input[type="+typ+"]") {
			t.Errorf("界面用了 type=%q 的输入框，但基础样式没覆盖它：\n  %s", typ, rule)
		}
	}
	if len(seen) == 0 {
		t.Fatal("没在 webUI 里找到任何带 type 的 input，测试本身失效了")
	}
}

// 宽度必须撑满，否则在 flex 行里两边会留出空隙，看着就是「短一截」。
func TestBaseInputStyleFillsWidth(t *testing.T) {
	i := strings.Index(webUI, "input[type=text]")
	if i < 0 {
		t.Fatal("找不到基础输入框样式")
	}
	end := strings.Index(webUI[i:], "}")
	if end < 0 {
		t.Fatal("基础输入框样式没有闭合的 }")
	}
	decl := webUI[i : i+end]
	for _, want := range []string{"width:100%", "padding:", "font-size:", "border-radius:"} {
		if !strings.Contains(decl, want) {
			t.Errorf("基础输入框样式缺少 %q：\n  %s", want, decl)
		}
	}
}
