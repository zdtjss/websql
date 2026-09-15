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

type QueryResult struct {
	Columns []string
	Data    []map[string]any
}

// maxExportRows 导出工具的行数硬上限（读取上限 + 1 行用于判定是否超限）。
// 导出无行数上限时，一条 `SELECT * FROM 大表` 会把整表读进内存并写盘，
// 既可能 OOM 也会生成超大文件，因此超过上限直接拒绝并提示模型先聚合/过滤。
const maxExportRows = 100_000

func QueryForExport(conn *sqlx.DB, sql string) (*QueryResult, error) {
	sql = strings.TrimSpace(sql)
	if sql == "" {
		return nil, errors.New("SQL 不能为空")
	}
	stripped := sqlutil.StripSQLComments(sql)
	upper := strings.ToUpper(stripped)
	if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "WITH") {
		return nil, errors.New("导出仅支持 SELECT 查询")
	}
	if strings.HasPrefix(upper, "WITH") {
		// 词级扫描写关键字：可识别被 CTE 包裹的写操作，且不受空白字符变体（制表符/换行）影响
		if sqlutil.HasWriteKeyword(sql) {
			return nil, errors.New("导出查询不允许包含写操作")
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

	data, err := database.GetResultRowsForExport(conn.DriverName(), rows, maxExportRows+1)
	if err != nil {
		return nil, fmt.Errorf("获取查询结果失败：%w", err)
	}
	if len(data) > maxExportRows {
		return nil, fmt.Errorf("导出数据量超过上限（%d 行）。请先用聚合（GROUP BY/COUNT）或 WHERE 条件缩小范围，或分批导出", maxExportRows)
	}
	return &QueryResult{Columns: cols, Data: data}, nil
}

// SanitizeFileName 生成安全的导出文件名（不含扩展名）。
//
// 安全要求：模型/用户可控的 fileName 必须被限制为「纯文件名」，禁止任何目录成分，
// 否则 `../../x` 之类的输入会导致导出的写盘操作逃逸出 exports 目录（任意文件写入）。
// 处理步骤：
//  1. 剥离已知扩展名
//  2. 用 filepath.Base 去掉目录部分（Unix/Windows 分隔符均处理）
//  3. 逐字符剔除路径分隔符、盘符冒号、控制字符
//  4. 去掉首尾空白与点，防止 "." / ".." / 隐藏文件
//  5. 兜底为 defaultPrefix，并限制长度
func SanitizeFileName(name, defaultPrefix string) string {
	timestamp := time.Now().Format("20060102_150405")
	for _, ext := range []string{".xlsx", ".xls", ".csv", ".docx", ".pptx", ".png", ".jpg", ".html"} {
		name = strings.TrimSuffix(name, ext)
	}
	// 先按两种分隔符取最后一段，兼容跨平台（filepath.Base 在 Linux 不识别 "\"）
	name = name[strings.LastIndexAny(name, `/\`)+1:]
	name = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	name = strings.Trim(name, " .")
	if name == "" {
		name = defaultPrefix
	}
	if runes := []rune(name); len(runes) > 80 {
		name = string(runes[:80])
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