package xfetch

import (
	"encoding/json"
	"net/url"
	"os"
	"strings"

	"superfetch/internal/xcore"
)

// Cookie 与 Playwright storageState 的 Cookie 字段一致。
type Cookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires"`
	HTTPOnly bool    `json:"httpOnly"`
	Secure   bool    `json:"secure"`
	SameSite string  `json:"sameSite"`
}

// LocalStorageItem 本地存储项。
type LocalStorageItem struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Origin 一个 origin 的本地存储。
type Origin struct {
	Origin       string             `json:"origin"`
	LocalStorage []LocalStorageItem `json:"localStorage"`
}

// StorageState 与 Playwright storageState 互通。
type StorageState struct {
	Cookies []Cookie `json:"cookies"`
	Origins []Origin `json:"origins"`
}

// LoadStorageState 读取会话文件，自动识别三种常见格式（storageState / Cookie-Editor 列表 / 键值对）。
func LoadStorageState(path string) (*StorageState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	xcore.SecureFile(path)

	var probe any
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, err
	}
	state := &StorageState{Cookies: []Cookie{}, Origins: []Origin{}}

	switch v := probe.(type) {
	case map[string]any:
		if _, ok := v["cookies"]; ok {
			if err := json.Unmarshal(data, state); err != nil {
				return nil, err
			}
			if state.Origins == nil {
				state.Origins = []Origin{}
			}
			return state, nil
		}
		// 简单键值对：视为 cookie 名值，domain 由调用方补
		for k, val := range v {
			s, ok := val.(string)
			if !ok {
				continue
			}
			state.Cookies = append(state.Cookies, Cookie{Name: k, Value: s, Path: "/", Expires: -1, SameSite: "Lax"})
		}
		return state, nil
	case []any:
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			ck := Cookie{Path: "/", Expires: -1, SameSite: "Lax"}
			if s, ok := m["name"].(string); ok {
				ck.Name = s
			}
			if s, ok := m["value"].(string); ok {
				ck.Value = s
			}
			if s, ok := m["domain"].(string); ok {
				ck.Domain = s
			}
			if s, ok := m["path"].(string); ok && s != "" {
				ck.Path = s
			}
			if f, ok := m["expirationDate"].(float64); ok {
				ck.Expires = f
			}
			if b, ok := m["httpOnly"].(bool); ok {
				ck.HTTPOnly = b
			}
			if b, ok := m["secure"].(bool); ok {
				ck.Secure = b
			}
			if s, ok := m["sameSite"].(string); ok {
				ck.SameSite = normalizeSameSite(s)
			}
			state.Cookies = append(state.Cookies, ck)
		}
		return state, nil
	}
	return state, nil
}

func normalizeSameSite(s string) string {
	switch strings.ToLower(strings.ReplaceAll(s, "_", "")) {
	case "norestriction", "none":
		return "None"
	case "strict":
		return "Strict"
	default:
		return "Lax"
	}
}

// SetSessionCookiesFor 把键值对注入为某域名的 Cookie（预热用）。
func (s *StorageState) AddDomainCookies(jar map[string]string, domain string) {
	for k, v := range jar {
		s.Cookies = append(s.Cookies, Cookie{Name: k, Value: v, Domain: domain, Path: "/", Expires: -1, SameSite: "Lax"})
	}
}

// SaveStorageState 原子写入会话文件并收紧权限。
func SaveStorageState(path string, state *StorageState) error {
	if state.Origins == nil {
		state.Origins = []Origin{}
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := xcore.AtomicWriteFile(path, data); err != nil {
		return err
	}
	xcore.SecureFile(path)
	return nil
}

// AutoMatchSession 在数据目录里按域名找已保存的登录态。
func AutoMatchSession(rawURL string) (string, bool) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	entries, err := os.ReadDir(xcore.DataDir())
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		stem := strings.ToLower(strings.TrimSuffix(e.Name(), ".json"))
		if len(stem) >= 3 && strings.Contains(host, stem) {
			return xcore.ResolveSessionPath(e.Name()), true
		}
	}
	return "", false
}
