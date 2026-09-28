package sqlguard

import (
	"errors"
	"fmt"
	"strings"
)

// 本文件实现 AI 只读通道（query_data / 导出取数 / 报告数据绑定）与写通道审批判定
// 共用的「词法级」SQL 安全校验。
//
// 为什么不复用 StripComments + 首关键字判断：
//   - MySQL 可执行注释 /*!50000 DELETE FROM t */ 会被 StripComments 当作普通注释剥掉，
//     剥离后首关键字为空 → 旧逻辑判定「非危险」→ exec_sql 跳过审批直接执行；
//   - SELECT ... INTO OUTFILE / LOAD_FILE() / FOR UPDATE / EXPLAIN ANALYZE ...
//     首关键字均为只读关键字，旧的只读白名单无法识别其副作用；
//   - 不同数据库对反斜杠转义、`--` 注释、Oracle q'[...]' 字符串的规则不同，
//     单一解释的扫描器存在「扫描器认为是字符串、数据库认为是代码」的错位，可借此隐藏关键字。
//
// 实现策略：
//  1. 用两套词法规则（MySQL 规则 / 标准 SQL + Oracle 规则）分别切词，任一规则下违规即拒绝；
//  2. 对「外传/读写服务器文件」类高危函数名做一次无视引号的原文扫描，彻底消除错位风险。
//
// 宁可多拦一次（模型可改写 SQL 或给标识符加引号），也不因解释差异漏放行。

// lexResult 单次词法扫描的结果
type lexResult struct {
	words          []string // 代码区（非字符串/引号标识符/注释）内的单词，已转大写
	nextSignif     []byte   // 每个单词之后第一个非空白字符（0 表示结尾）
	execComment    bool     // 出现 MySQL 可执行注释 /*! ... */
	multiStatement bool     // 出现「裸分号 + 后续有效内容」
}

func isWordStart(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b >= 0x80
}

func isWordByte(b byte) bool {
	return isIdentByte(b) || b >= 0x80
}

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' || b == '\v'
}

// oracleQuoteClose 返回 Oracle q'<d>...<d'>' 字符串的闭合定界符
func oracleQuoteClose(open byte) byte {
	switch open {
	case '[':
		return ']'
	case '(':
		return ')'
	case '{':
		return '}'
	case '<':
		return '>'
	}
	return open
}

// lexSQL 按指定规则切词。
//
//	mysqlRules=true ：反斜杠在字符串内转义；`-- ` 需后跟空白才是注释；`#` 为行注释
//	mysqlRules=false：反斜杠无特殊含义；`--` 总是注释；支持 Oracle q'[...]' 字符串
func lexSQL(sql string, mysqlRules bool) lexResult {
	var r lexResult
	n := len(sql)
	i := 0
	sawSemicolon := false

	skipQuoted := func(q byte) {
		i++ // 跳过起始引号
		for i < n {
			c := sql[i]
			if mysqlRules && c == '\\' && q != '`' {
				i += 2
				continue
			}
			if c == q {
				if i+1 < n && sql[i+1] == q { // '' / "" / `` 转义
					i += 2
					continue
				}
				i++
				return
			}
			i++
		}
	}

	for i < n {
		c := sql[i]
		switch {
		case c == '\'' || c == '"' || c == '`':
			if sawSemicolon {
				r.multiStatement = true
			}
			skipQuoted(c)
			continue
		case c == '/' && i+1 < n && sql[i+1] == '*':
			if i+2 < n && sql[i+2] == '!' {
				r.execComment = true
			}
			end := strings.Index(sql[i+2:], "*/")
			if end < 0 {
				i = n
			} else {
				i += end + 4
			}
			continue
		case c == '-' && i+1 < n && sql[i+1] == '-':
			isComment := true
			if mysqlRules && i+2 < n {
				isComment = isSpaceByte(sql[i+2])
			}
			if isComment {
				end := strings.IndexByte(sql[i:], '\n')
				if end < 0 {
					i = n
				} else {
					i += end + 1
				}
				continue
			}
		case c == '#' && mysqlRules:
			end := strings.IndexByte(sql[i:], '\n')
			if end < 0 {
				i = n
			} else {
				i += end + 1
			}
			continue
		case c == ';':
			sawSemicolon = true
			i++
			continue
		case isWordStart(c):
			if sawSemicolon {
				r.multiStatement = true
			}
			j := i + 1
			for j < n && isWordByte(sql[j]) {
				j++
			}
			word := strings.ToUpper(sql[i:j])
			// Oracle 替代引号：q'[...]' / nq'{...}'
			if !mysqlRules && (word == "Q" || word == "NQ") && j+1 < n && sql[j] == '\'' {
				closeDelim := oracleQuoteClose(sql[j+1])
				k := j + 2
				for k+1 < n && !(sql[k] == closeDelim && sql[k+1] == '\'') {
					k++
				}
				i = k + 2
				continue
			}
			r.words = append(r.words, word)
			k := j
			for k < n && isSpaceByte(sql[k]) {
				k++
			}
			if k < n {
				r.nextSignif = append(r.nextSignif, sql[k])
			} else {
				r.nextSignif = append(r.nextSignif, 0)
			}
			i = j
			continue
		case c > ' ' && sawSemicolon:
			// 分号之后出现任何可见字符（括号、数字等）都视为第二条语句
			r.multiStatement = true
		}
		i++
	}
	return r
}

// readOnlyFirstWords 只读通道允许的首关键字
var readOnlyFirstWords = map[string]bool{
	"SELECT": true, "WITH": true, "SHOW": true, "DESCRIBE": true, "DESC": true,
	"EXPLAIN": true, "VALUES": true, "TABLE": true,
}

// forbiddenWords 只读语句代码区内出现即拒绝的关键字
var forbiddenWords = map[string]string{
	"DELETE": "写操作 DELETE", "UPDATE": "写操作或行锁 UPDATE（含 FOR UPDATE）",
	"DROP": "DDL DROP", "ALTER": "DDL ALTER", "CREATE": "DDL CREATE",
	"RENAME": "DDL RENAME", "GRANT": "权限变更 GRANT", "REVOKE": "权限变更 REVOKE",
	"CALL": "存储过程调用 CALL", "EXEC": "动态执行 EXEC", "EXECUTE": "动态执行 EXECUTE",
	"LOCK": "锁语句 LOCK", "UNLOCK": "锁语句 UNLOCK", "HANDLER": "HANDLER 语句",
	"LOAD": "LOAD DATA", "SHUTDOWN": "SHUTDOWN", "DBMS_SQL": "动态 SQL DBMS_SQL",
}

// functionOnlyWords 仅当作为函数调用（后跟左括号）时允许：
// INSERT(str,pos,len,new) / REPLACE(str,a,b) / TRUNCATE(x,d) 是常用的标量函数。
var functionOnlyWords = map[string]string{
	"INSERT": "写操作 INSERT", "REPLACE": "写操作 REPLACE", "TRUNCATE": "DDL TRUNCATE",
	"MERGE": "写操作 MERGE",
}

// neverAnywhereWords 读写服务器文件/网络外传类高危能力：无论是否位于引号内，出现即拒绝。
// 原文扫描不区分字符串，用于消除不同词法解释之间的错位风险。
var neverAnywhereWords = map[string]string{
	"OUTFILE": "写服务器文件 INTO OUTFILE", "DUMPFILE": "写服务器文件 INTO DUMPFILE",
	"LOAD_FILE": "读服务器文件 LOAD_FILE()", "LOAD_EXTENSION": "加载扩展 load_extension()",
	"UTL_HTTP": "网络外传 UTL_HTTP", "UTL_TCP": "网络外传 UTL_TCP", "UTL_SMTP": "网络外传 UTL_SMTP",
	"UTL_FILE": "文件读写 UTL_FILE", "UTL_INADDR": "网络探测 UTL_INADDR", "HTTPURITYPE": "网络外传 HTTPURITYPE",
	"DBMS_PIPE": "DBMS_PIPE", "DBMS_SCHEDULER": "DBMS_SCHEDULER", "DBMS_JAVA": "DBMS_JAVA",
	"SYS_EXEC": "sys_exec()", "SYS_EVAL": "sys_eval()",
}

const quoteHint = "（若为列名或表名，请用引号包裹后重试）"

// checkReadOnlyWords 对单次词法结果做只读判定
func checkReadOnlyWords(r lexResult) error {
	if r.execComment {
		return errors.New("不允许使用 MySQL 可执行注释（/*! ... */），其内容会被数据库执行")
	}
	if r.multiStatement {
		return errors.New("只允许单条 SQL 语句（检测到分号分隔的多条语句）")
	}
	if len(r.words) == 0 {
		return errors.New("无法识别的 SQL 语句")
	}
	first := r.words[0]
	if !readOnlyFirstWords[first] {
		return fmt.Errorf("只读通道仅支持 SELECT/WITH/SHOW/DESCRIBE/EXPLAIN 查询，当前语句以 %s 开头", first)
	}
	// SHOW / DESCRIBE 语句本身只读（SHOW CREATE TABLE 等需要保留）
	if first == "SHOW" || first == "DESCRIBE" || first == "DESC" {
		return nil
	}
	for idx, w := range r.words {
		if reason, bad := forbiddenWords[w]; bad {
			return fmt.Errorf("查询包含不允许的操作：%s%s", reason, quoteHint)
		}
		if reason, bad := functionOnlyWords[w]; bad && r.nextSignif[idx] != '(' {
			return fmt.Errorf("查询包含不允许的操作：%s%s", reason, quoteHint)
		}
		switch w {
		case "INTO":
			// SELECT ... INTO @var 无副作用；INTO 表/文件一律拒绝
			if r.nextSignif[idx] != '@' {
				return errors.New("查询包含不允许的操作：SELECT ... INTO（仅允许 INTO @变量）")
			}
		case "SHARE":
			if idx > 0 && r.words[idx-1] == "FOR" {
				return errors.New("查询包含不允许的操作：FOR SHARE 行锁")
			}
		case "ANALYZE":
			// EXPLAIN ANALYZE 会真实执行被解释的语句
			if first == "EXPLAIN" {
				return errors.New("不允许使用 EXPLAIN ANALYZE（会真实执行语句），请使用 EXPLAIN")
			}
		}
	}
	return nil
}

// checkNeverAnywhere 原文扫描高危函数名（不区分引号/注释）
func checkNeverAnywhere(sql string) error {
	n := len(sql)
	for i := 0; i < n; {
		if !isWordStart(sql[i]) {
			i++
			continue
		}
		j := i + 1
		for j < n && isWordByte(sql[j]) {
			j++
		}
		if reason, bad := neverAnywhereWords[strings.ToUpper(sql[i:j])]; bad {
			return fmt.Errorf("查询包含不允许的操作：%s", reason)
		}
		i = j
	}
	return nil
}

// ValidateReadOnlyQuery 校验 SQL 是否为「可证明无副作用」的单条只读查询。
//
// 用于 AI 智能体的所有只读取数通道：query_data、导出工具的 sql 参数、
// 报告中 ```chart / ```table 数据块绑定的 SQL。返回 nil 表示放行。
func ValidateReadOnlyQuery(sql string) error {
	if strings.TrimSpace(sql) == "" {
		return errors.New("SQL 不能为空")
	}
	if err := checkNeverAnywhere(sql); err != nil {
		return err
	}
	for _, mysqlRules := range []bool{true, false} {
		if err := checkReadOnlyWords(lexSQL(sql, mysqlRules)); err != nil {
			return err
		}
	}
	return nil
}

// HasExecutableComment 判断 SQL 是否包含 MySQL 可执行注释（/*! ... */）。
// 字符串字面量内的 "/*!" 不计入。
func HasExecutableComment(sql string) bool {
	return lexSQL(sql, true).execComment || lexSQL(sql, false).execComment
}

// RequiresApproval 判断写通道（exec_sql）中的语句是否需要人工审批。
//
// fail-closed：凡不能证明是无副作用只读查询的语句都需要审批——
// 不仅是 INSERT/UPDATE/DELETE/DDL，也包括 CALL、SET、RENAME、LOCK、
// 以及首关键字无法识别的语句（如被可执行注释包裹的写操作）。
func RequiresApproval(sql string) bool {
	return ValidateReadOnlyQuery(sql) != nil
}
