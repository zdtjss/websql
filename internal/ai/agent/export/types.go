package export

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"websql/internal/ai/agent/sqlutil"
	database "websql/internal/database"

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
	qr, truncated, err := queryForExport(conn, sql, maxExportRows)
	if err != nil {
		return nil, err
	}
	if truncated {
		return nil, fmt.Errorf("导出数据量超过上限（%d 行）。请先用聚合（GROUP BY/COUNT）或 WHERE 条件缩小范围，或分批导出", maxExportRows)
	}
	return qr, nil
}

// QueryForExportLimit 执行只读导出查询，最多返回 limit 行（超出部分静默截断）。
// 用于报告内嵌数据绑定（明细表/图表）等只展示前 N 行的场景。
func QueryForExportLimit(conn *sqlx.DB, sql string, limit int) (*QueryResult, error) {
	if limit <= 0 || limit > maxExportRows {
		limit = maxExportRows
	}
	qr, _, err := queryForExport(conn, sql, limit)
	return qr, err
}

// queryForExport 执行导出查询并读取至多 limit 行；truncated 表示结果超出 limit 被截断。
func queryForExport(conn *sqlx.DB, sql string, limit int) (qr *QueryResult, truncated bool, err error) {
	if conn == nil {
		return nil, false, errors.New("数据库连接不可用，无法执行导出查询")
	}
	sql = strings.TrimSpace(sql)
	if sql == "" {
		return nil, false, errors.New("SQL 不能为空")
	}
	stripped := sqlutil.StripSQLComments(sql)
	upper := strings.ToUpper(stripped)
	if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "WITH") {
		return nil, false, errors.New("导出仅支持 SELECT 查询")
	}
	if strings.HasPrefix(upper, "WITH") {
		// 词级扫描写关键字：可识别被 CTE 包裹的写操作，且不受空白字符变体（制表符/换行）影响
		if sqlutil.HasWriteKeyword(sql) {
			return nil, false, errors.New("导出查询不允许包含写操作")
		}
	}

	rows, err := conn.Queryx(sql)
	if err != nil {
		return nil, false, fmt.Errorf("查询失败：%w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, false, fmt.Errorf("获取列信息失败：%w", err)
	}

	data, err := database.GetResultRowsForExport(conn.DriverName(), rows, limit+1)
	if err != nil {
		return nil, false, fmt.Errorf("获取查询结果失败：%w", err)
	}
	if len(data) > limit {
		data = data[:limit]
		truncated = true
	}
	return &QueryResult{Columns: cols, Data: data}, truncated, nil
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
//  6. 追加「时间戳 + 随机后缀」形成不可枚举的能力 URL（下载侧另有归属校验兜底）
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
	return fmt.Sprintf("%s_%s_%s", name, timestamp, randomToken4())
}

// randomToken4 返回 8 位十六进制随机串（32bit），使导出 URL 不可枚举。
func randomToken4() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano()&0xFFFFFFFF, 16)
	}
	return hex.EncodeToString(b)
}

func EnsureExportsDir() {
	os.MkdirAll("exports", 0755)
}

// exportsDirAbs 返回导出目录的绝对路径（供子进程环境变量 WEBSQL_EXPORT_DIR 使用）。
func exportsDirAbs() string {
	if abs, err := filepath.Abs("exports"); err == nil {
		return abs
	}
	return "exports"
}

func ColIndex(cols []string, name string) int {
	for i, c := range cols {
		if c == name {
			return i
		}
	}
	return -1
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

type ExcelChartSeriesDef struct {
	YAxisField string `json:"yAxisField" jsonschema:"required" jsonschema_description:"Y 轴字段名"`
	Name       string `json:"name" jsonschema_description:"系列名称，用于图例显示"`
}

type ExcelChart struct {
	ChartType  string                `json:"chartType" jsonschema:"required" jsonschema_description:"图表类型: line, bar, pie, area, scatter, radar, stackedBar, doughnut"`
	XAxisField string                `json:"xAxisField" jsonschema:"required" jsonschema_description:"X 轴字段名（须与查询结果列名一致）"`
	Series     []ExcelChartSeriesDef `json:"series" jsonschema:"required" jsonschema_description:"Y 轴系列，可指定多个字段生成多系列图表"`
	ChartTitle string                `json:"chartTitle" jsonschema_description:"图表标题"`
}

type ExportExcelInput struct {
	SQL      string `json:"sql" jsonschema:"required" jsonschema_description:"用于导出的 SELECT SQL"`
	FileName string `json:"fileName" jsonschema_description:"文件名（不含扩展名）"`
	Title    string `json:"title" jsonschema_description:"表格标题（可选）"`
}

type ExportExcelOutput struct {
	Message     string `json:"message"`
	RowCount    int    `json:"rowCount"`
	DownloadURL string `json:"downloadUrl"`
	FileType    string `json:"fileType"`
}

type ExportExcelWithChartInput struct {
	SQL        string       `json:"sql" jsonschema:"required" jsonschema_description:"用于导出的 SELECT SQL"`
	FileName   string       `json:"fileName" jsonschema_description:"文件名（不含扩展名）"`
	Charts     []ExcelChart `json:"charts" jsonschema_description:"图表定义列表，每个元素在「图表」工作表生成一个原生可编辑图表。支持多条折线/柱状（通过 series 指定多个 Y 字段）"`
	ChartType  string       `json:"chartType" jsonschema_description:"单图兼容字段（建议改用 charts）：图表类型 bar/line/pie/area/scatter/radar 等"`
	XAxisField string       `json:"xAxisField" jsonschema_description:"单图兼容字段（建议改用 charts）：X 轴字段名"`
	YAxisField string       `json:"yAxisField" jsonschema_description:"单图兼容字段（建议改用 charts）：Y 轴字段名"`
	ChartTitle string       `json:"chartTitle" jsonschema_description:"图表标题（可选）"`
}

type ExportExcelWithChartOutput struct {
	Message     string `json:"message"`
	RowCount    int    `json:"rowCount"`
	ChartCount  int    `json:"chartCount"`
	DownloadURL string `json:"downloadUrl"`
	FileType    string `json:"fileType"`
}

// ExportReportInput 报告类导出统一入参（export_analysis_docx / export_ppt / export_html 共用）。
//
// 两种模式：
//   - content 模式（推荐）：直接传扩展 Markdown 正文，支持 ```kpi 指标卡、
//     ```chart 图表（可给 categories/series，也可给 sql 由服务端经权限校验取数）、
//     ```table 数据表、> [!NOTE] 提示框、$$公式$$、<!-- pagebreak --> 分页；
//   - sql 模式：仅给 SQL 时服务端自动生成「指标卡 + 图表 + 明细表」图文报告。
type ExportReportInput struct {
	Title    string `json:"title" jsonschema_description:"报告标题（显示在封面）"`
	Subtitle string `json:"subtitle" jsonschema_description:"副标题（可选）"`
	Theme    string `json:"theme" jsonschema_description:"主题：business(默认，商务蓝) | tech(深色科技风)"`
	Content  string `json:"content" jsonschema_description:"报告正文（扩展 Markdown，与 sql 二选一，推荐）。支持标题/段落/列表/表格/引用/kpi 指标卡/chart 图表/table 数据表/mermaid 图示/公式/分页"`
	FileName string `json:"fileName" jsonschema_description:"文件名（不含扩展名）"`
	SQL      string `json:"sql" jsonschema_description:"SELECT SQL（与 content 二选一，仅提供 SQL 时自动生成含指标卡/图表/明细的报告）"`
}

type ExportReportOutput struct {
	Message     string `json:"message"`
	SlideCount  int    `json:"slideCount,omitempty"`
	DownloadURL string `json:"downloadUrl"`
	FileType    string `json:"fileType"`
}
