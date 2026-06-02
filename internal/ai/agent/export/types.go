package export

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	database "websql/internal/database"
	"websql/internal/ai/agent/sqlutil"

	"github.com/jmoiron/sqlx"
)

// StripSQLComments 是 sqlutil.StripSQLComments 的本地别名（EINO_DEEP_ANALYSIS §10.1）。
//
// 历史上 export 包有自己的一份 StripSQLComments（按行过滤实现），
// 无法处理 /* ... */ 块注释和字符串内的 --。已统一到 sqlutil 包。
// 保留本别名仅为兼容 export 包内部历史调用点；新代码请直接用 sqlutil.StripSQLComments。
func StripSQLComments(sql string) string { return sqlutil.StripSQLComments(sql) }

type QueryResult struct {
	Columns []string
	Data    []map[string]any
}

func QueryForExport(conn *sqlx.DB, sql string) (*QueryResult, error) {
	sql = strings.TrimSpace(sql)
	if sql == "" {
		return nil, errors.New("SQL 不能为空")
	}
	stripped := StripSQLComments(sql)
	upper := strings.ToUpper(stripped)
	if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "WITH") {
		return nil, errors.New("导出仅支持 SELECT 查询")
	}
	if strings.HasPrefix(upper, "WITH") {
		writeKeywords := []string{"INSERT ", "UPDATE ", "DELETE ", "DROP ", "TRUNCATE ", "ALTER ", "CREATE ", "REPLACE ", "MERGE "}
		for _, kw := range writeKeywords {
			if strings.Contains(upper, kw) {
				return nil, fmt.Errorf("导出查询不允许包含写操作（%s）", strings.TrimSpace(kw))
			}
		}
	}

	rows, err := conn.Queryx(sql)
	if err != nil {
		return nil, fmt.Errorf("查询失败：%w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("获取列信息失败：%w", err)
	}

	data, err := database.GetResultRowsForExport(conn.DriverName(), rows)
	if err != nil {
		return nil, fmt.Errorf("获取查询结果失败：%w", err)
	}
	return &QueryResult{Columns: cols, Data: data}, nil
}

func SanitizeFileName(name, defaultPrefix string) string {
	timestamp := time.Now().Format("20060102_150405")
	if name == "" {
		return fmt.Sprintf("%s_%s", defaultPrefix, timestamp)
	}
	for _, ext := range []string{".xlsx", ".xls", ".csv", ".docx", ".pptx", ".png", ".jpg"} {
		name = strings.TrimSuffix(name, ext)
	}
	return fmt.Sprintf("%s_%s", name, timestamp)
}

func EnsureExportsDir() {
	os.MkdirAll("exports", 0755)
}

func ColIndex(cols []string, name string) int {
	for i, c := range cols {
		if c == name {
			return i
		}
	}
	return -1
}

func ColLetter(idx int) string {
	result := ""
	for {
		result = string(rune('A'+idx%26)) + result
		idx = idx/26 - 1
		if idx < 0 {
			break
		}
	}
	return result
}

func ToFloat64(v any) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case float32:
		return float64(val), nil
	case int:
		return float64(val), nil
	case int64:
		return float64(val), nil
	case int32:
		return float64(val), nil
	case string:
		return strconv.ParseFloat(val, 64)
	case []byte:
		return strconv.ParseFloat(string(val), 64)
	default:
		s := fmt.Sprintf("%v", v)
		return strconv.ParseFloat(s, 64)
	}
}

func DetectNumericCols(qr *QueryResult) []string {
	if len(qr.Data) == 0 {
		return nil
	}
	var numeric []string
	for _, col := range qr.Columns {
		if _, err := ToFloat64(qr.Data[0][col]); err == nil {
			numeric = append(numeric, col)
		}
	}
	return numeric
}

func CalcNumericStats(qr *QueryResult, col string) (min, max, avg float64, count int) {
	min = 1e18
	max = -1e18
	var sum float64
	for _, row := range qr.Data {
		if v, err := ToFloat64(row[col]); err == nil {
			sum += v
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
			count++
		}
	}
	if count > 0 {
		avg = sum / float64(count)
	}
	return
}

func ExtractXYSeries(qr *QueryResult, xCol, yCol string) (xVals, yVals []float64, labels []string) {
	xIdx := ColIndex(qr.Columns, xCol)
	yIdx := ColIndex(qr.Columns, yCol)
	if xIdx < 0 || yIdx < 0 {
		return nil, nil, nil
	}

	for i, row := range qr.Data {
		yVal, err := ToFloat64(row[qr.Columns[yIdx]])
		if err != nil {
			yVal = 0
		}
		xVals = append(xVals, float64(i))
		yVals = append(yVals, yVal)
		labels = append(labels, fmt.Sprintf("%v", row[qr.Columns[xIdx]]))
	}
	return
}

func FindFieldIndex(columns []string, field string) int {
	idx := ColIndex(columns, field)
	if idx >= 0 {
		return idx
	}
	lower := strings.ToLower(field)
	for i, c := range columns {
		if strings.Contains(strings.ToLower(c), lower) {
			return i
		}
	}
	return -1
}

func ExcelAutoWidth(qr *QueryResult, colIdx int) float64 {
	maxW := float64(len([]rune(qr.Columns[colIdx]))) * 1.5
	for _, row := range qr.Data {
		val := fmt.Sprintf("%v", row[qr.Columns[colIdx]])
		w := float64(len([]rune(val))) * 1.2
		if w > maxW {
			maxW = w
		}
	}
	if maxW < 8 {
		return 8
	}
	if maxW > 36 {
		return 36
	}
	return maxW + 2
}

func NumberFormatter(v any) string {
	if f, ok := v.(float64); ok {
		if f >= 1e7 {
			return fmt.Sprintf("%.1fM", f/1e6)
		}
		if f >= 1e4 {
			return fmt.Sprintf("%.1fK", f/1e3)
		}
		return fmt.Sprintf("%.0f", f)
	}
	return fmt.Sprintf("%v", v)
}

type ExcelChartSeriesDef struct {
	YAxisField string `json:"yAxisField" jsonschema:"required" jsonschema_description:"Y 轴字段名"`
	Name       string `json:"name" jsonschema_description:"系列名称，用于图例显示"`
}

type ExcelChart struct {
	ChartType  string                `json:"chartType" jsonschema:"required" jsonschema_description:"图表类型: line, bar, pie, scatter, area, stackedBar, doughnut, radar"`
	XAxisField string                `json:"xAxisField" jsonschema:"required" jsonschema_description:"X 轴字段名"`
	Series     []ExcelChartSeriesDef `json:"series" jsonschema:"required" jsonschema_description:"Y 轴系列，可指定多个字段生成多系列图表"`
	ChartTitle string                `json:"chartTitle" jsonschema_description:"图表标题"`
	SheetName  string                `json:"sheetName" jsonschema_description:"图表所在 Sheet 名称，为空则自动分配"`
}

type ExportExcelInput struct {
	SQL      string `json:"sql" jsonschema:"required" jsonschema_description:"用于导出的 SELECT SQL"`
	FileName string `json:"fileName" jsonschema_description:"文件名（不含扩展名）"`
}

type ExportExcelOutput struct {
	Message     string `json:"message"`
	RowCount    int    `json:"rowCount"`
	DownloadURL string `json:"downloadUrl"`
	FileType    string `json:"fileType"`
}

type ExportExcelWithChartInput struct {
	SQL      string       `json:"sql" jsonschema:"required" jsonschema_description:"用于导出的 SELECT SQL"`
	FileName string       `json:"fileName" jsonschema_description:"文件名（不含扩展名）"`
	Charts   []ExcelChart `json:"charts" jsonschema_description:"图表定义列表，每个元素会在独立 Sheet 中生成一个图表。支持多条折线/柱状（通过 series 指定多个 Y 字段）"`
	ChartType  string `json:"chartType" jsonschema_description:"图表类型: line, bar, pie, scatter, area, stackedBar, doughnut, radar"`
	XAxisField string `json:"xAxisField" jsonschema_description:"X 轴字段名"`
	YAxisField string `json:"yAxisField" jsonschema_description:"Y 轴字段名"`
	ChartTitle string `json:"chartTitle" jsonschema_description:"图表标题"`
}

type ExportExcelWithChartOutput struct {
	Message     string `json:"message"`
	RowCount    int    `json:"rowCount"`
	DownloadURL string `json:"downloadUrl"`
	FileType    string `json:"fileType"`
}

type ExportPPTInput struct {
	SQL       string `json:"sql" jsonschema_description:"用于导出的 SELECT SQL（与 content 二选一，仅 Excel 必须提供 SQL）"`
	Content   string `json:"content" jsonschema_description:"演示内容（Markdown 格式，与 sql 二选一。按 ## 标题自动分页）"`
	FileName  string `json:"fileName" jsonschema_description:"文件名（不含扩展名）"`
	Title     string `json:"title" jsonschema_description:"PPT 标题"`
	SlideType string `json:"slideType" jsonschema_description:"幻灯片类型: summary, table, chart"`
}

type ExportPPTOutput struct {
	Message     string `json:"message"`
	SlideCount  int    `json:"slideCount"`
	DownloadURL string `json:"downloadUrl"`
	FileType    string `json:"fileType"`
}

type ExportAnalysisImageInput struct {
	SQL        string `json:"sql" jsonschema:"required" jsonschema_description:"用于导出的 SELECT SQL"`
	FileName   string `json:"fileName" jsonschema_description:"文件名（不含扩展名）"`
	ChartType  string `json:"chartType" jsonschema_description:"图表类型: line, bar, pie"`
	XAxisField string `json:"xAxisField" jsonschema:"required" jsonschema_description:"X 轴字段名"`
	YAxisField string `json:"yAxisField" jsonschema:"required" jsonschema_description:"Y 轴字段名"`
	Title      string `json:"title" jsonschema_description:"图表标题"`
}

type ExportAnalysisImageOutput struct {
	Message     string `json:"message"`
	DownloadURL string `json:"downloadUrl"`
	FileType    string `json:"fileType"`
	Format      string `json:"format"`
}

type ExportAnalysisDocxInput struct {
	SQL          string `json:"sql" jsonschema_description:"用于导出的 SELECT SQL（与 content 二选一，仅 Excel 必须提供 SQL）"`
	Content      string `json:"content" jsonschema_description:"分析报告内容（Markdown 格式，与 sql 二选一。可包含标题、段落、列表、代码块、mermaid 图表等）"`
	FileName     string `json:"fileName" jsonschema_description:"文件名（不含扩展名）"`
	Title        string `json:"title" jsonschema_description:"报告标题"`
	IncludeChart bool   `json:"includeChart" jsonschema_description:"是否包含图表（仅 sql 模式有效）"`
}

type ExportAnalysisDocxOutput struct {
	Message     string `json:"message"`
	DownloadURL string `json:"downloadUrl"`
	FileType    string `json:"fileType"`
}