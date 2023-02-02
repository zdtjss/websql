package utils

import (
	"go-web/logutils"
	"strconv"
	"strings"
)

func ExtractSql(s string) string {
	relSql := strings.TrimSpace(s)
	if relSql == "" || strings.HasPrefix(relSql, "--") || strings.HasPrefix(relSql, "//") || strings.HasPrefix(relSql, "/*") {
		nsql := []string{}
		for _, row := range strings.Split(relSql, "\n") {
			if row == "" || strings.HasPrefix(row, "--") || strings.HasPrefix(row, "//") || strings.HasPrefix(row, "/*") {
				continue
			}
			nsql = append(nsql, row)
		}
		relSql = strings.Join(nsql, "\n")
	}
	return relSql
}

func AtoUint64(s string) uint64 {
	uis, err := strconv.ParseUint(s, 10, 64)
	logutils.Panicln(err)
	return uis
}
