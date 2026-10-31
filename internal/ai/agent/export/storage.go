package export

// 导出文件归属登记（任务 7.1）。
//
// 背景：`/exports/:filename` 下载接口此前仅校验登录态，文件名含时间戳可枚举，
// 多用户部署下用户 A 能下载用户 B 的报告（含数据明细）。
// 方案（能力 URL + 归属登记双保险）：
//   1. SanitizeFileName 追加随机后缀，URL 不可枚举；
//   2. 生成成功后在 exports/.owners/<file>.owner 落地归属用户 ID，
//      下载侧比对当前用户，不符返回与「文件不存在」一致的 404（避免探测）；
//   3. 无归属记录的历史文件按兼容策略放行，随 7 天清理淘汰；
//   4. 清理任务同时清理孤儿 .owner 记录（CleanupOrphanOwners）。

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

const exportsURLPrefix = "/exports/"

// ownerFilePath 返回文件名对应的归属记录路径（含目录）。
func ownerFilePath(fileName string) string {
	return filepath.Join(ownersDir(), fileName+".owner")
}

// safeExportBaseName 校验文件名是「纯文件名」（无目录成分），防止归属记录写入逃逸。
func safeExportBaseName(fileName string) bool {
	if fileName == "" || strings.ContainsAny(fileName, `/\`) || strings.Contains(fileName, "..") {
		return false
	}
	return true
}

// RecordExportOwnerByURL 记录导出文件归属（url 形如 /exports/xxx.docx）。
// userID 为空（无用户上下文的本地/旁路调用）时跳过登记：下载侧按兼容策略放行。
func RecordExportOwnerByURL(url, userID string) {
	fileName := strings.TrimPrefix(url, exportsURLPrefix)
	if fileName == url { // 非 /exports/ 前缀，无法定位文件
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" || !safeExportBaseName(fileName) {
		return
	}
	if err := os.MkdirAll(ownersDir(), 0755); err != nil {
		log.Printf("[ExportOwner] 创建归属目录失败 - err=%v\n", err)
		return
	}
	if err := os.WriteFile(ownerFilePath(fileName), []byte(userID), 0644); err != nil {
		log.Printf("[ExportOwner] 写入归属记录失败 - file=%s, err=%v\n", fileName, err)
	}
}

// CheckExportAccess 校验用户能否下载指定导出文件。
// 返回 (allowed, known)：known=false 表示无归属记录（历史文件/经典导出链路），
// 调用方应按兼容策略放行。
func CheckExportAccess(fileName, userID string) (allowed bool, known bool) {
	if !safeExportBaseName(fileName) {
		return false, false
	}
	data, err := os.ReadFile(ownerFilePath(fileName))
	if err != nil {
		return false, false
	}
	owner := strings.TrimSpace(string(data))
	if owner == "" {
		return false, false
	}
	return owner == strings.TrimSpace(userID), true
}

// CleanupOrphanOwners 清理孤儿归属记录（对应导出文件已被删除/清理）。
// 由导出目录清理任务调用。
func CleanupOrphanOwners() {
	entries, err := os.ReadDir(ownersDir())
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".owner") {
			continue
		}
		fileName := strings.TrimSuffix(entry.Name(), ".owner")
		if !safeExportBaseName(fileName) {
			_ = os.Remove(filepath.Join(ownersDir(), entry.Name()))
			continue
		}
		if _, err := os.Stat(Path(fileName)); os.IsNotExist(err) {
			_ = os.Remove(filepath.Join(ownersDir(), entry.Name()))
		}
	}
}
