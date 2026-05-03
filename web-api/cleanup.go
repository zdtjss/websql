package webapi

import (
	"log"
	"os"
	"path/filepath"
	"time"
)

func StartCleanupScheduler() {
	go func() {
		for {
			time.Sleep(1 * time.Hour)
			cleanExpiredExports()
		}
	}()
	log.Println("[Cleanup] 导出文件清理任务已启动（每1小时清理超过7天的文件）")
}

func cleanExpiredExports() {
	dir := "exports"
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[Cleanup] 读取导出目录失败: %v\n", err)
		}
		return
	}

	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	deleted := 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			path := filepath.Join(dir, entry.Name())
			if err := os.Remove(path); err != nil {
				log.Printf("[Cleanup] 删除失败 %s: %v\n", path, err)
			} else {
				deleted++
			}
		}
	}

	if deleted > 0 {
		log.Printf("[Cleanup] 已清理 %d 个过期导出文件\n", deleted)
	}
}
