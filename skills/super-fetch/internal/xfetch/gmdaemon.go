package xfetch

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type gmRequest struct {
	URL      string  `json:"url"`
	Session  string  `json:"session"`
	Timeout  int     `json:"timeout"`
	Wait     float64 `json:"wait"`
	Selector string  `json:"selector"`
}

type gmResponse struct {
	HTML  string `json:"html"`
	Error string `json:"error"`
}

func gmSock(backend string) string {
	if s := os.Getenv("SF_DAEMON_SOCK"); s != "" {
		return s
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = "/tmp"
	}
	return filepath.Join(dir, "sf-"+backend+"-"+strconv.Itoa(os.Getuid())+".sock")
}

// dshHostPID 向上找 DSH 宿主进程，daemon 随它退出。
func dshHostPID() int {
	pid := os.Getpid()
	for i := 0; i < 12; i++ {
		data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		if err != nil {
			return 0
		}
		// comm 字段可能含空格/括号，取最后一个 ')' 之后
		s := string(data)
		idx := strings.LastIndex(s, ")")
		if idx < 0 {
			return 0
		}
		fields := strings.Fields(s[idx+1:])
		if len(fields) < 2 {
			return 0
		}
		ppid, _ := strconv.Atoi(fields[1])
		if ppid <= 1 {
			return 0
		}
		cmd, _ := os.ReadFile("/proc/" + strconv.Itoa(ppid) + "/cmdline")
		c := strings.ReplaceAll(string(cmd), "\x00", " ")
		if strings.Contains(c, "dsh") && (strings.Contains(c, "web") || strings.Contains(c, "bin.ts")) {
			return ppid
		}
		pid = ppid
	}
	return 0
}

// tryDaemon 发一条请求；daemon 不可用返回 false。
func tryDaemon(backend, url, session, selector string, timeout int, wait float64) (string, bool) {
	conn, err := net.DialTimeout("unix", gmSock(backend), 300*time.Millisecond)
	if err != nil {
		return "", false
	}
	defer conn.Close()
	req := gmRequest{URL: url, Session: session, Timeout: timeout, Wait: wait, Selector: selector}
	data, _ := json.Marshal(req)
	_ = conn.SetDeadline(time.Now().Add(time.Duration(timeout+60) * time.Second))
	if _, err := conn.Write(append(data, '\n')); err != nil {
		return "", false
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return "", false
	}
	var resp gmResponse
	if json.Unmarshal(line, &resp) != nil || resp.Error != "" || resp.HTML == "" {
		return "", false
	}
	return resp.HTML, true
}

// killOtherDaemons 保证同时只有一个浏览器 daemon（这台机器内存只有 6G）。
func killOtherDaemons(keep string) {
	for _, b := range []string{"gomoufox", "nodriver", "chromedp"} {
		if b == keep {
			continue
		}
		data, err := os.ReadFile(gmSock(b) + ".pid")
		if err != nil {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil || pid <= 0 {
			continue
		}
		// daemon 用 Setsid 启动，pgid == pid：先 SIGTERM 让它自己关浏览器，再兜底 SIGKILL
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		time.Sleep(500 * time.Millisecond)
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
}

// ensureDaemon 起一个常驻 daemon（已有则跳过），最多等 60 秒就绪。
func ensureDaemon(backend string) bool {
	if _, ok := tryDaemonPing(backend); ok {
		return true
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	sfd := filepath.Join(filepath.Dir(exe), "sfd")
	if _, err := os.Stat(sfd); err != nil {
		if p, err := exec.LookPath("sfd"); err == nil {
			sfd = p
		} else {
			return false
		}
	}
	killOtherDaemons(backend)
	// 同后端旧 daemon 也先收掉，避免残留浏览器
	if data, err := os.ReadFile(gmSock(backend) + ".pid"); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
			_ = syscall.Kill(-pid, syscall.SIGTERM)
			time.Sleep(400 * time.Millisecond)
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	}
	cmd := exec.Command(sfd, "--backend", backend, "--sock", gmSock(backend), "--dsh-pid", strconv.Itoa(dshHostPID()))
	cmd.SysProcAttr = sysProcAttrDetach()
	logPath := filepath.Join(os.TempDir(), "sfd-"+backend+".log")
	if lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
		cmd.Stdout, cmd.Stderr = lf, lf
	}
	_ = cmd.Start()
	return waitDaemon(backend, 60*time.Second)
}

func tryDaemonPing(backend string) (string, bool) {
	conn, err := net.DialTimeout("unix", gmSock(backend), 200*time.Millisecond)
	if err != nil {
		return "", false
	}
	conn.Close()
	return "ok", true
}

func waitDaemon(backend string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if _, ok := tryDaemonPing(backend); ok {
			return true
		}
		time.Sleep(300 * time.Millisecond)
	}
	return false
}

// FetchViaDaemon 走常驻 daemon；SF_NO_DAEMON=1 时禁用。
func FetchViaDaemon(backend, url, session, selector string, timeout int, wait float64) (string, bool) {
	if os.Getenv("SF_NO_DAEMON") == "1" || backend == "" {
		return "", false
	}
	if html, ok := tryDaemon(backend, url, session, selector, timeout, wait); ok {
		return html, true
	}
	if !ensureDaemon(backend) {
		return "", false
	}
	return tryDaemon(backend, url, session, selector, timeout, wait)
}
