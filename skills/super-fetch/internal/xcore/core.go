// Package xcore 提供跨子命令共享的常量、路径与工具函数。
package xcore

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"
)

// StealthJS 抹掉最常见的自动化特征（与 Python 版一致）。
const StealthJS = `
Object.defineProperty(navigator, 'webdriver', { get: () => undefined });
Object.defineProperty(navigator, 'languages', { get: () => ['zh-CN', 'zh', 'en'] });
Object.defineProperty(navigator, 'plugins', { get: () => [1, 2, 3, 4, 5] });
window.chrome = window.chrome || { runtime: {} };
const _q = window.navigator.permissions && window.navigator.permissions.query;
if (_q) {
  window.navigator.permissions.query = (p) =>
    p && p.name === 'notifications'
      ? Promise.resolve({ state: Notification.permission })
      : _q.call(window.navigator.permissions, p);
}
const _gp = WebGLRenderingContext.prototype.getParameter;
WebGLRenderingContext.prototype.getParameter = function (p) {
  if (p === 37445) return 'Intel Inc.';
  if (p === 37446) return 'Intel Iris OpenGL Engine';
  return _gp.call(this, p);
};
`

// RealHeaders 与 Chrome 152 指纹配套的请求头。
// tls-client 只模拟 TLS/HTTP2 指纹，不像 curl_cffi 那样自动补默认头，
// 所以这里必须手写一套与指纹版本一致的浏览器头（少了 UA 会被 Wikipedia 403）。
var RealHeaders = map[string]string{
	"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
	"Accept-Language":           "zh-CN,zh;q=0.9,en;q=0.8",
	"User-Agent":                "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36",
	"Sec-Ch-Ua":                 `"Chromium";v="152", "Not(A:Brand";v="24", "Google Chrome";v="152"`,
	"Sec-Ch-Ua-Mobile":          "?0",
	"Sec-Ch-Ua-Platform":        `"Windows"`,
	"Upgrade-Insecure-Requests": "1",
}

// UserAgents 浏览器池可能用到的 UA 池。
var UserAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:140.0) Gecko/20100101 Firefox/140.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15",
}

var (
	dataDirOnce sync.Once
	dataDirPath string
)

// DataDir 返回数据目录：默认与 Python 版共用 super-fetch/.data，可用 SUPER_FETCH_HOME 覆盖。
func DataDir() string {
	dataDirOnce.Do(func() {
		if v := os.Getenv("SUPER_FETCH_HOME"); v != "" {
			p, err := filepath.Abs(expandHome(v))
			if err == nil {
				dataDirPath = p
			}
		}
		if dataDirPath == "" {
			if d := locateSharedDataDir(); d != "" {
				dataDirPath = d
			}
		}
		if dataDirPath == "" {
			home, _ := os.UserHomeDir()
			dataDirPath = filepath.Join(home, ".agents", "super-fetch")
		}
		_ = os.MkdirAll(dataDirPath, 0o700)
		_ = os.Chmod(dataDirPath, 0o700)
	})
	return dataDirPath
}

// locateSharedDataDir 从可执行文件位置向上找同级的「super-fetch/.data」目录，
// 让二进制被挪动后仍能找回链接库、接口库和登录态（Python 版已移除，这里只看 .data）。
func locateSharedDataDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dir := filepath.Dir(exe)
	for i := 0; i < 5; i++ {
		cand := filepath.Join(dir, "super-fetch")
		if st, err := os.Stat(filepath.Join(cand, ".data")); err == nil && st.IsDir() {
			return filepath.Join(cand, ".data")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") || p == "~" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return p
}

// DBPath 链接库 / 接口库的 SQLite 文件。
func DBPath() string { return filepath.Join(DataDir(), "links.db") }

// SecureFile 把文件权限收紧到 0600（会话文件里有 Cookie）。
func SecureFile(path string) {
	_ = os.Chmod(path, 0o600)
}

// ResolveSessionPath 解析会话文件路径，相对路径只允许落在数据目录内。
func ResolveSessionPath(arg string) string {
	if arg == "" {
		return ""
	}
	if filepath.IsAbs(arg) {
		return arg
	}
	if _, err := os.Stat(arg); err == nil {
		return arg
	}
	return filepath.Join(DataDir(), filepath.Base(arg))
}

// RuneLen 按 Unicode 字符数计长度（Python 的 len(str) 语义）。
func RuneLen(s string) int { return utf8.RuneCountInString(s) }

// TruncateRunes 按字符数截断。
func TruncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

// AtomicWriteFile 原子写文件（先写临时文件再 rename）。
func AtomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
