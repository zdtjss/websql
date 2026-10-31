package report

import (
	"strings"
	"testing"
)

// TestParseMarkdown_SpansAndIDs 块必须带稳定 ID 与源行区间，供块级编辑定位。
func TestParseMarkdown_SpansAndIDs(t *testing.T) {
	src := "# 标题\n\n" +
		"## 第一节\n\n" +
		"第一段第一行\n第一段第二行\n\n" +
		"| 列A | 列B |\n|---|---|\n| 1 | 2 |\n\n" +
		"```kpi\n[{\"label\":\"指标\",\"value\":\"1\"}]\n```\n\n" +
		"> [!NOTE] 备注\n> 内容\n"
	blocks := ParseMarkdown(src)
	if len(blocks) < 5 {
		t.Fatalf("解析块过少：%d", len(blocks))
	}
	lines := strings.Split(src, "\n")
	seen := map[string]bool{}
	for i, b := range blocks {
		if b.ID == "" {
			t.Fatalf("块 %d 缺少 ID", i)
		}
		if seen[b.ID] {
			t.Fatalf("块 ID 重复：%s", b.ID)
		}
		seen[b.ID] = true
		if b.StartLine < 1 || b.EndLine < b.StartLine || b.EndLine > len(lines) {
			t.Fatalf("块 %s 行区间异常：[%d,%d]", b.ID, b.StartLine, b.EndLine)
		}
		if strings.TrimSpace(strings.Join(lines[b.StartLine-1:b.EndLine], "\n")) == "" {
			t.Errorf("块 %s 行区间指向空内容", b.ID)
		}
	}
	// 段落跨两行
	for _, b := range blocks {
		if b.Kind == KindParagraph && strings.Contains(b.Text, "第一段第一行") {
			if b.EndLine-b.StartLine != 1 {
				t.Errorf("段落行区间应为 2 行，实际 [%d,%d]", b.StartLine, b.EndLine)
			}
		}
	}
}

// TestBuildOutline_Sections 章节树应包含块 ID 与结构统计。
func TestBuildOutline_Sections(t *testing.T) {
	doc := BuildDocument("报告", "", "# 报告\n\n## 一\n\n正文\n\n| A | B |\n|---|---|\n| 1 | 2 |\n\n## 二\n\n结论\n")
	out := BuildOutline(doc)
	if out == nil || len(out.Sections) != 2 {
		t.Fatalf("章节数异常：%+v", out)
	}
	if out.Sections[0].HeadingID == "" {
		t.Error("章节缺少 headingId")
	}
	if out.Summary.Tables != 1 {
		t.Errorf("表格统计异常：%d", out.Summary.Tables)
	}
	if len(out.Sections[0].Blocks) == 0 || out.Sections[0].Blocks[0].ID == "" {
		t.Errorf("章节块缺少 ID：%+v", out.Sections[0])
	}
}

// TestPlanSlides_Mapping 页映射必须覆盖封面/正文/致谢，且块区间合法。
func TestPlanSlides_Mapping(t *testing.T) {
	content := "# 报告\n\n" +
		"## 第一部分\n\n第一部分正文。\n\n" +
		"## 第二部分\n\n第二部分正文。\n\n" +
		"## 第三部分\n\n第三部分正文。\n"
	doc := BuildDocument("报告", "", content)
	slides := PlanSlides(doc)
	if len(slides) < 3 {
		t.Fatalf("页数过少：%d", len(slides))
	}
	if slides[0].Kind != "cover" || slides[len(slides)-1].Kind != "closing" {
		t.Errorf("首尾页类型异常：first=%s last=%s", slides[0].Kind, slides[len(slides)-1].Kind)
	}
	hasBlock := false
	for _, s := range slides {
		if s.Index < 1 || s.Index > len(slides) {
			t.Errorf("页序号非法：%+v", s)
		}
		if s.StartBlock >= 0 {
			hasBlock = true
			if s.StartBlock > s.EndBlock || s.EndBlock >= len(doc.Blocks) {
				t.Errorf("页块区间非法：%+v（块数 %d）", s, len(doc.Blocks))
			}
		}
	}
	if !hasBlock {
		t.Error("应至少有一页映射到源块")
	}
}

// TestTableToMarkdown 表格行编辑后应能还原为可再次解析的 Markdown。
func TestTableToMarkdown(t *testing.T) {
	tb := &Table{Headers: []string{"A", "B"}, Rows: [][]string{{"1", "2"}, {"3", "4"}}, Align: []string{"left", "right"}}
	md := TableToMarkdown(tb)
	blocks := ParseMarkdown(md)
	if len(blocks) != 1 || blocks[0].Kind != KindTable || blocks[0].Table == nil {
		t.Fatalf("还原后的表格无法解析：%q", md)
	}
	if len(blocks[0].Table.Rows) != 2 || blocks[0].Table.Rows[1][0] != "3" {
		t.Errorf("表格内容不一致：%+v", blocks[0].Table)
	}
}
