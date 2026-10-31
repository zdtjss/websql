package agent

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestMaskText_BuiltinRules(t *testing.T) {
	m := NewNoopMasker()
	cases := []struct {
		name string
		in   string
		not  string
	}{
		{"手机号", "联系电话 13800138000", "13800138000"},
		{"身份证", "证件 110101199001011234 已登记", "110101199001011234"},
		{"邮箱", "邮箱 zhangsan@example.com", "zhangsan@example.com"},
		{"password", "password=hunter2", "hunter2"},
		{"引号密钥", `api_key: "sk-abcdef123456"`, "sk-abcdef123456"},
		{"单引号密钥", `secret='top-secret-value'`, "top-secret-value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := m.MaskText(tc.in)
			if strings.Contains(got, tc.not) {
				t.Fatalf("敏感内容未脱敏：%q", got)
			}
			if again := m.MaskText(got); again != got {
				t.Fatalf("脱敏应幂等：once=%q twice=%q", got, again)
			}
		})
	}
}

func TestMaskText_PreservesNormalChinesePunctuation(t *testing.T) {
	m := NewNoopMasker()
	input := "这是一段正常说明，包含中文逗号。"
	if got := m.MaskText(input); got != input {
		t.Fatalf("正常文本不应被改写：got=%q want=%q", got, input)
	}
}

func TestMaskText_DecodesBase64SensitiveToken(t *testing.T) {
	m := NewNoopMasker()
	encoded := base64.StdEncoding.EncodeToString([]byte("13800138000"))
	got := m.MaskText("编码值 " + encoded + " 结束")
	if strings.Contains(got, encoded) {
		t.Fatalf("命中敏感规则的编码 token 应整体替换：%q", got)
	}
}

func TestMaskText_KeepsNonSensitiveBase64(t *testing.T) {
	m := NewNoopMasker()
	encoded := base64.StdEncoding.EncodeToString([]byte("hello-world"))
	got := m.MaskText("普通链接 " + encoded)
	if !strings.Contains(got, encoded) {
		t.Fatalf("非敏感编码内容不应被改写：%q", got)
	}
}

func TestMaskQueryResult_UsesOriginalColumnRules(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	m, err := NewDataMasker([]MaskRule{
		{ConnID: "c1", Schema: "s1", Table: "users", Column: "password", Action: MaskRedact},
		{ConnID: "c1", Schema: "s1", Table: "users", Column: "phone", Action: MaskPartial},
	}, key, "v1")
	if err != nil {
		t.Fatalf("构建脱敏器失败：%v", err)
	}
	payload, _ := json.Marshal(QueryOutput{
		Columns: []string{"x", "phone"},
		Data: []map[string]any{
			{"x": "p@ssw0rd", "phone": "13800138000"},
		},
		Count: 1,
	})
	origins := map[string]ResourceRef{
		"x":     {ConnID: "c1", Schema: "s1", Table: "users", Column: "password"},
		"phone": {ConnID: "c1", Schema: "s1", Table: "users", Column: "phone"},
	}
	got, err := m.MaskQueryResult(string(payload), []string{"s1.users"}, origins, nil)
	if err != nil {
		t.Fatalf("脱敏失败：%v", err)
	}
	var out QueryOutput
	if err := json.Unmarshal([]byte(got), &out); err != nil {
		t.Fatalf("结果解析失败：%v", err)
	}
	if out.Data[0]["x"] != "***" {
		t.Fatalf("别名列应按原始列规则 redact，实际：%v", out.Data[0]["x"])
	}
	if out.Data[0]["phone"] == "13800138000" {
		t.Fatalf("phone 应被部分脱敏，实际：%v", out.Data[0]["phone"])
	}
}

func TestMaskQueryResult_UnknownOriginRedactedWhenRulesExist(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	m, err := NewDataMasker([]MaskRule{
		{ConnID: "c1", Schema: "s1", Table: "users", Column: "password", Action: MaskRedact},
	}, key, "v1")
	if err != nil {
		t.Fatalf("构建脱敏器失败：%v", err)
	}
	// 列名命中规则列（users.password）但来源不可解析 → 整列 redact
	payload, _ := json.Marshal(QueryOutput{
		Columns: []string{"password"},
		Data:    []map[string]any{{"password": "secret-value"}},
		Count:   1,
	})
	got, err := m.MaskQueryResult(string(payload), nil, map[string]ResourceRef{}, nil)
	if err != nil {
		t.Fatalf("脱敏失败：%v", err)
	}
	if strings.Contains(got, "secret-value") {
		t.Fatalf("列名命中参查表规则时应 redact：%q", got)
	}
	// 列名与规则无关时不得被错误抹除（否则 SELECT * 全列变 ***）
	unrelated, _ := json.Marshal(QueryOutput{
		Columns: []string{"unknown_col"},
		Data:    []map[string]any{{"unknown_col": "ordinary-value"}},
		Count:   1,
	})
	got2, err := m.MaskQueryResult(string(unrelated), []string{"s1.users"}, map[string]ResourceRef{}, nil)
	if err != nil {
		t.Fatalf("脱敏失败：%v", err)
	}
	if !strings.Contains(got2, "ordinary-value") {
		t.Fatalf("与规则列无关的输出列不应被抹除：%q", got2)
	}
}

func TestDataMasker_TokenizeIsKeyedAndDeterministic(t *testing.T) {
	rules := []MaskRule{{ConnID: "c1", Table: "t", Column: "uid", Action: MaskTokenize}}
	m1, err := NewDataMasker(rules, []byte("key-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), "v1")
	if err != nil {
		t.Fatalf("构建失败：%v", err)
	}
	m2, err := NewDataMasker(rules, []byte("key-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"), "v1")
	if err != nil {
		t.Fatalf("构建失败：%v", err)
	}
	v1 := m1.maskWithAction(MaskTokenize, "user-1")
	v1again := m1.maskWithAction(MaskTokenize, "user-1")
	v2 := m2.maskWithAction(MaskTokenize, "user-1")
	if v1 != v1again {
		t.Fatal("同密钥同值应得到相同 token")
	}
	if v1 == v2 {
		t.Fatal("不同密钥应得到不同 token（不可跨环境关联）")
	}
	if !strings.HasPrefix(v1.(string), "tok_") {
		t.Fatalf("token 前缀错误：%v", v1)
	}
}

func TestDataMasker_TokenizeRequiresKey(t *testing.T) {
	if _, err := NewDataMasker([]MaskRule{
		{ConnID: "c1", Table: "t", Column: "uid", Action: MaskTokenize},
	}, nil, "v1"); err == nil {
		t.Fatal("缺少 HMAC 密钥时应拒绝加载 tokenize 规则")
	}
}
