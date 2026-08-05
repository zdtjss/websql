package sync

// splitSQLStatements 按引号感知的方式切分多语句 SQL 文本。
//
// 与前端 DataSyncDialog.splitSQLStatements 逻辑保持一致：
//   - 识别单引号字符串（含 '' 转义），字符串内的分号不作为分隔符
//   - 识别行注释（-- ...）与块注释（/* ... */），注释内的分号不作为分隔符
//   - 仅在字符串/注释之外的分号处切分
//
// 修复此前使用 strings.Split(sql, ";") 导致数据值含分号时语句被误切的问题。
func splitSQLStatements(sqlText string) []string {
	statements := make([]string, 0, 64)
	var current []byte
	inSingleQuote := false
	inLineComment := false
	inBlockComment := false

	flush := func() {
		trimmed := bytesTrimSpace(current)
		if len(trimmed) > 0 {
			statements = append(statements, string(trimmed))
		}
		current = current[:0]
	}

	for i := 0; i < len(sqlText); i++ {
		ch := sqlText[i]
		var next byte
		if i+1 < len(sqlText) {
			next = sqlText[i+1]
		}

		// 行注释开始
		if !inSingleQuote && !inBlockComment && ch == '-' && next == '-' {
			inLineComment = true
			current = append(current, ch)
			continue
		}
		if inLineComment {
			current = append(current, ch)
			if ch == '\n' {
				inLineComment = false
			}
			continue
		}

		// 块注释开始
		if !inSingleQuote && ch == '/' && next == '*' {
			inBlockComment = true
			current = append(current, ch, next)
			i++
			continue
		}
		if inBlockComment {
			current = append(current, ch)
			if ch == '*' && next == '/' {
				current = append(current, next)
				i++
				inBlockComment = false
			}
			continue
		}

		// 单引号字符串（'' 为转义）
		if ch == '\'' {
			if inSingleQuote && next == '\'' {
				current = append(current, '\'', '\'')
				i++
				continue
			}
			inSingleQuote = !inSingleQuote
			current = append(current, ch)
			continue
		}

		// 分号分隔（不在字符串/注释中）
		if ch == ';' && !inSingleQuote {
			flush()
			continue
		}

		current = append(current, ch)
	}

	flush()
	return statements
}

// bytesTrimSpace 去除首尾空白字符（避免引入 strings 依赖混淆，行为等同 strings.TrimSpace）
func bytesTrimSpace(b []byte) []byte {
	start, end := 0, len(b)
	for start < end && isSpaceByte(b[start]) {
		start++
	}
	for end > start && isSpaceByte(b[end-1]) {
		end--
	}
	return b[start:end]
}

func isSpaceByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f'
}
