package logger

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"websql/internal/pkg/safego"

	"github.com/gin-gonic/gin"
)

// dailyRotateWriter 按天切换日志文件，文件名格式 <prefix>-YYYY-MM-DD.log。
// 跨天首次写入时自动关闭旧文件、打开新文件，并触发过期日志清理。
type dailyRotateWriter struct {
	dir         string
	prefix      string
	maxAgeDays  int
	currentDate string // "2006-01-02"
	currentFile *os.File
	mu          sync.Mutex
}

// Init 在启动最早阶段调用，初始化按天轮转日志：
//   - 创建日志目录
//   - 将标准 log 输出切换为按天轮转 writer
//   - 立即清理一次过期日志
//   - 启动每日清理 goroutine
//
// logDir 为日志目录；prefix 为文件名前缀（如 "websql"）；maxAgeDays 为保留天数，<=0 时不清理。
// 失败时回退到 stderr，不阻断启动。
func Init(logDir, prefix string, maxAgeDays int) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		log.Printf("[Logger] 创建日志目录失败 %s: %v，回退到 stderr", logDir, err)
		return
	}
	w := &dailyRotateWriter{
		dir:        logDir,
		prefix:     prefix,
		maxAgeDays: maxAgeDays,
	}
	if err := w.openToday(); err != nil {
		log.Printf("[Logger] 打开日志文件失败: %v，回退到 stderr", err)
		return
	}
	log.SetOutput(w)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	gin.DefaultWriter = w
	gin.DefaultErrorWriter = w

	w.cleanOldLogs()
	if maxAgeDays > 0 {
		safego.GoWithName("log-cleaner", func() {
			ticker := time.NewTicker(24 * time.Hour)
			defer ticker.Stop()
			for range ticker.C {
				w.cleanOldLogs()
			}
		})
	}
	log.Printf("[Logger] 日志按天轮转已启用，目录=%s，保留=%d天", logDir, maxAgeDays)
}

func (w *dailyRotateWriter) openToday() error {
	today := time.Now().Format("2006-01-02")
	path := filepath.Join(w.dir, fmt.Sprintf("%s-%s.log", w.prefix, today))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	w.currentDate = today
	w.currentFile = f
	return nil
}

// Write 实现 io.Writer。每次写入检查日期，跨天则切换文件。
func (w *dailyRotateWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	today := time.Now().Format("2006-01-02")
	if today != w.currentDate {
		if err := w.rotateLocked(today); err != nil {
			// 切换失败时兜底写 stderr，避免日志丢失阻塞调用方
			fmt.Fprintf(os.Stderr, "[Logger] 切换日志文件失败: %v\n", err)
			return os.Stderr.Write(p)
		}
	}
	if w.currentFile == nil {
		return os.Stderr.Write(p)
	}
	return w.currentFile.Write(p)
}

// rotateLocked 关闭旧文件并打开新日期文件，调用方持有 w.mu。
func (w *dailyRotateWriter) rotateLocked(today string) error {
	if w.currentFile != nil {
		w.currentFile.Close()
		w.currentFile = nil
	}
	path := filepath.Join(w.dir, fmt.Sprintf("%s-%s.log", w.prefix, today))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	w.currentDate = today
	w.currentFile = f
	// 切换时清理过期日志
	go w.cleanOldLogs()
	return nil
}

// cleanOldLogs 删除早于 maxAgeDays 的日志文件。文件名需匹配 <prefix>-YYYY-MM-DD.log。
func (w *dailyRotateWriter) cleanOldLogs() {
	if w.maxAgeDays <= 0 {
		return
	}
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -w.maxAgeDays)
	ext := ".log"
	prefixWithDash := w.prefix + "-"
	deleted := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, prefixWithDash) || !strings.HasSuffix(name, ext) {
			continue
		}
		dateStr := strings.TrimSuffix(strings.TrimPrefix(name, prefixWithDash), ext)
		t, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue // 非日期文件名，跳过不删
		}
		if t.Before(cutoff) {
			if err := os.Remove(filepath.Join(w.dir, name)); err == nil {
				deleted++
			}
		}
	}
	if deleted > 0 {
		log.Printf("[Logger] 已清理 %d 个过期日志文件（保留 %d 天）", deleted, w.maxAgeDays)
	}
}

func PrintErr(err error) {
	if err != nil {
		log.Println(err)
	}
}

func PrintErrf(format string, err error, msg ...any) {
	if err == nil {
		return
	}
	if len(msg) == 0 {
		log.Printf(format+" err : %s\n", err.Error())

	} else {
		msg = append(msg, err.Error())
		log.Printf(format+" err : %s\n", msg...)
	}
}

func PanicErr(err error) {
	if err != nil {
		log.Panicln(err)
	}
}

func PanicErrf(format string, err error, msg ...any) {
	if err == nil {
		return
	}
	if len(msg) == 0 {
		log.Panicf(format+" err : %s \n", err.Error())
	} else {
		msg = append(msg, err.Error())
		log.Panicf(format+" err : %s\n", msg...)
	}
}
