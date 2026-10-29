//go:build windows

package export

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// P2 OS 级隔离（Windows）：低完整性令牌 + 运行目录低完整性标签。
//
// 原理：
//   - 以「低完整性（Low IL）」令牌创建 codegen 子进程：在默认强制策略下，
//     低完整性进程可读取中完整性对象（Python 安装目录、脚本、受信助手等），
//     但**无法写入**中完整性对象（用户文件、系统目录、配置等）；
//   - 把本次运行的 runDir 及其子对象标注为低完整性（NO_WRITE_UP + 继承），
//     使子进程只能写 runDir，形成「写仅限自己创建的目录」的 OS 级保证。
//
// 兜底：若令牌创建或 CreateProcessAsUser 失败（权限/能力不足），本平台视为
// 「无可用隔离」，调用方 fail-closed 不注册高级导出工具。

const (
	seGroupIntegrity = 0x00000020 // SE_GROUP_INTEGRITY

	aclRevision             = 2
	aceTypeMandatoryLabel   = 0x11 // SYSTEM_MANDATORY_LABEL_ACE_TYPE
	mandatoryLabelNoWriteUp = 0x01 // SYSTEM_MANDATORY_LABEL_NO_WRITE_UP
	aceFlagObjectInherit    = 0x01
	aceFlagContainerInherit = 0x02
)

var (
	isolationOnce   sync.Once
	isolationUsable bool
)

// codegenIsolationName 返回当前平台的隔离机制名称（审计/日志用）。
func codegenIsolationName() string { return "windows-low-integrity" }

// codegenIsolationAvailable 探测当前环境是否可用低完整性隔离（结果缓存）。
func codegenIsolationAvailable() bool {
	isolationOnce.Do(func() {
		isolationUsable = probeLowIntegrityIsolation()
		if !isolationUsable {
			log.Printf("[Codegen] 低完整性隔离不可用，高级导出将不予注册（fail-closed）\n")
		}
	})
	return isolationUsable
}

// isolateCodegenRunDir 为运行目录施加隔离前置条件：Windows 下即标注低完整性。
// 目录 ACL 必须授予 WRITE_OWNER（完全控制）；仅有 Modify 权限时 SetNamedSecurityInfo
// 会返回 Access is denied，由调用方切换到下一个候选根。
func isolateCodegenRunDir(dir string) error {
	return labelDirLowIntegrity(dir)
}

// probeLowIntegrityIsolation 端到端探测：在候选运行根上依次验证「建目录 → 标注低完整性
// → 以低完整性令牌启动子进程」；任一候选根可用即视为隔离可用（与运行时选择逻辑一致）。
func probeLowIntegrityIsolation() bool {
	python := GetPythonPath()
	if python == "" {
		return false
	}
	token, err := createLowIntegrityToken()
	if err != nil {
		return false
	}
	defer token.Close()

	for _, base := range codegenRunBaseDirs() {
		if err := os.MkdirAll(base, 0o700); err != nil {
			continue
		}
		dir, err := os.MkdirTemp(base, "websql_codegen_probe_")
		if err != nil {
			continue
		}
		if err := labelDirLowIntegrity(dir); err != nil {
			_ = os.RemoveAll(dir)
			continue
		}

		cmd := exec.Command(python, "-c", "import sys; sys.exit(0)")
		hideWindow(cmd)
		cmd.Dir = dir
		cmd.SysProcAttr.Token = syscall.Token(uintptr(token))
		runErr := cmd.Run()
		_ = os.RemoveAll(dir)
		if runErr == nil {
			return true
		}
	}
	return false
}

// applyCodegenIsolation 对 codegen 子进程应用低完整性隔离。
// 返回的 cleanup 用于在进程结束后释放令牌句柄。
func applyCodegenIsolation(cmd *exec.Cmd, runDir string) (func(), error) {
	if err := labelDirLowIntegrity(runDir); err != nil {
		return nil, fmt.Errorf("标注运行目录低完整性失败：%w", err)
	}
	token, err := createLowIntegrityToken()
	if err != nil {
		return nil, fmt.Errorf("创建低完整性令牌失败：%w", err)
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Token = syscall.Token(uintptr(token))
	return func() { _ = token.Close() }, nil
}

// createLowIntegrityToken 从当前进程令牌派生一个低完整性主令牌。
func createLowIntegrityToken() (windows.Token, error) {
	var current windows.Token
	if err := windows.OpenProcessToken(
		windows.CurrentProcess(),
		windows.TOKEN_DUPLICATE|windows.TOKEN_QUERY|windows.TOKEN_ADJUST_DEFAULT,
		&current,
	); err != nil {
		return 0, fmt.Errorf("OpenProcessToken: %w", err)
	}
	defer current.Close()

	var low windows.Token
	if err := windows.DuplicateTokenEx(
		current,
		windows.MAXIMUM_ALLOWED,
		nil,
		windows.SecurityImpersonation,
		windows.TokenPrimary,
		&low,
	); err != nil {
		return 0, fmt.Errorf("DuplicateTokenEx: %w", err)
	}

	sid, err := windows.CreateWellKnownSid(windows.WinLowLabelSid)
	if err != nil {
		low.Close()
		return 0, fmt.Errorf("CreateWellKnownSid(Low): %w", err)
	}

	tml := windows.Tokenmandatorylabel{
		Label: windows.SIDAndAttributes{Sid: sid, Attributes: seGroupIntegrity},
	}
	if err := windows.SetTokenInformation(
		low,
		windows.TokenIntegrityLevel,
		(*byte)(unsafe.Pointer(&tml)),
		tml.Size(),
	); err != nil {
		low.Close()
		return 0, fmt.Errorf("SetTokenInformation(IntegrityLevel): %w", err)
	}
	return low, nil
}

// labelDirLowIntegrity 把目录标注为低完整性（NO_WRITE_UP），并使子对象继承该标签。
func labelDirLowIntegrity(dir string) error {
	sid, err := windows.CreateWellKnownSid(windows.WinLowLabelSid)
	if err != nil {
		return fmt.Errorf("CreateWellKnownSid(Low): %w", err)
	}
	acl, keep := buildMandatoryLabelSACL(sid)
	err = windows.SetNamedSecurityInfo(
		dir,
		windows.SE_FILE_OBJECT,
		windows.LABEL_SECURITY_INFORMATION,
		nil, nil, nil, acl,
	)
	runtime.KeepAlive(keep)
	return err
}

// buildMandatoryLabelSACL 构造仅含一个「强制标签 ACE」的 SACL：
//
//	ACL 头(8B) + SYSTEM_MANDATORY_LABEL_ACE(4B 头 + 4B Mask + SID)
//
// 返回值第二个为底层缓冲，调用方需保证其在 SetNamedSecurityInfo 调用期间存活。
func buildMandatoryLabelSACL(sid *windows.SID) (*windows.ACL, []byte) {
	sidLen := sid.Len()
	aceSize := 4 + 4 + sidLen
	aclSize := 8 + aceSize

	buf := make([]byte, aclSize)
	// ACL 头
	buf[0] = aclRevision
	buf[1] = 0
	binary.LittleEndian.PutUint16(buf[2:4], uint16(aclSize))
	binary.LittleEndian.PutUint16(buf[4:6], 1) // AceCount
	binary.LittleEndian.PutUint16(buf[6:8], 0) // Sbz2

	// 强制标签 ACE
	off := 8
	buf[off] = aceTypeMandatoryLabel
	buf[off+1] = aceFlagObjectInherit | aceFlagContainerInherit
	binary.LittleEndian.PutUint16(buf[off+2:off+4], uint16(aceSize))
	binary.LittleEndian.PutUint32(buf[off+4:off+8], mandatoryLabelNoWriteUp)
	copy(buf[off+8:], unsafe.Slice((*byte)(unsafe.Pointer(sid)), sidLen))

	return (*windows.ACL)(unsafe.Pointer(&buf[0])), buf
}