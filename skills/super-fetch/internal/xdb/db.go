// Package xdb 负责链接库与接口库（与 Python 版共用一个 links.db）。
package xdb

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"strings"

	"github.com/PuerkitoBio/goquery"
	_ "modernc.org/sqlite"

	"superfetch/internal/xcore"
)

func open() (*sql.DB, error) {
	dsn := "file:" + xcore.DBPath() + "?_pragma=busy_timeout(15000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// InitDB 初始化链接库，并按 5% 概率清理 7 天前的旧记录。
func InitDB() {
	db, err := open()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[*] ⚠️ 数据库初始化异常: %v\n", err)
		return
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS links
		(id TEXT PRIMARY KEY, url TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		fmt.Fprintf(os.Stderr, "[*] ⚠️ 数据库初始化异常: %v\n", err)
		return
	}
	if rand.Float64() < 0.05 {
		_, _ = db.Exec("DELETE FROM links WHERE created_at <= datetime('now', '-7 days')")
	}
	xcore.SecureFile(xcore.DBPath())
}

// NewNamespace 生成 4 位命名空间（小写字母+数字）。
func NewNamespace() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 4)
	for i := range b {
		b[i] = alphabet[rand.Intn(len(alphabet))]
	}
	return string(b)
}

// LinkPair 一条链接代号 → 真实地址。
type LinkPair struct {
	ID  string
	URL string
}

// SaveLinks 批量写入链接库。
func SaveLinks(pairs []LinkPair) {
	if len(pairs) == 0 {
		return
	}
	db, err := open()
	if err != nil {
		return
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return
	}
	stmt, err := tx.Prepare("INSERT OR REPLACE INTO links (id, url) VALUES (?, ?)")
	if err != nil {
		tx.Rollback()
		return
	}
	defer stmt.Close()
	for _, p := range pairs {
		_, _ = stmt.Exec(p.ID, p.URL)
	}
	_ = tx.Commit()
}

// ProcessLinksToIDs 把 a[href] 与 img[src] 换成 @ns-id，返回命名空间。
func ProcessLinksToIDs(root *goquery.Selection, baseURL string) string {
	InitDB()
	ns := NewNamespace()
	var pairs []LinkPair
	counter := 0

	root.Find("a, img").Each(func(_ int, s *goquery.Selection) {
		attr := "href"
		if goquery.NodeName(s) == "img" {
			attr = "src"
		}
		link, ok := s.Attr(attr)
		if !ok {
			return
		}
		link = strings.TrimSpace(link)
		if link == "" ||
			strings.HasPrefix(link, "javascript:") ||
			strings.HasPrefix(link, "mailto:") ||
			strings.HasPrefix(link, "#") ||
			strings.HasPrefix(link, "data:") {
			return
		}
		abs, err := resolveURL(baseURL, link)
		if err != nil || abs == "" {
			return
		}
		counter++
		id := fmt.Sprintf("%s-%d", ns, counter)
		s.SetAttr(attr, "@"+id)
		pairs = append(pairs, LinkPair{ID: id, URL: abs})
	})
	SaveLinks(pairs)
	return ns
}

func resolveURL(base, ref string) (string, error) {
	b, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	r, err := url.Parse(ref)
	if err != nil {
		return "", err
	}
	return b.ResolveReference(r).String(), nil
}

// LinksByNamespace 按命名空间取链接对照表。
func LinksByNamespace(ns string, limit int) []LinkPair {
	db, err := open()
	if err != nil {
		return nil
	}
	defer db.Close()
	rows, err := db.Query(
		"SELECT id, url FROM links WHERE id LIKE ? ORDER BY id LIMIT ?",
		ns+"-%", limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []LinkPair
	for rows.Next() {
		var p LinkPair
		if rows.Scan(&p.ID, &p.URL) == nil {
			out = append(out, p)
		}
	}
	return out
}

// LookupLinks 精确查询若干代号（get_link 用）。
func LookupLinks(ids []string) map[string]string {
	out := map[string]string{}
	db, err := open()
	if err != nil {
		return out
	}
	defer db.Close()
	for _, id := range ids {
		var u string
		if err := db.QueryRow("SELECT url FROM links WHERE id = ?", id).Scan(&u); err == nil {
			out[id] = u
		}
	}
	return out
}

// ClearLinks 清理链接：ids 为空时清空全部（needConfirm 语义由调用方保证）。
func ClearLinks(ids []string) (int64, error) {
	db, err := open()
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var affected int64
	if len(ids) == 0 {
		res, err := db.Exec("DELETE FROM links")
		if err != nil {
			return 0, err
		}
		affected, _ = res.RowsAffected()
	} else {
		for _, raw := range ids {
			id := strings.TrimPrefix(raw, "@")
			var res sql.Result
			var err error
			if strings.Contains(id, "-") {
				res, err = db.Exec("DELETE FROM links WHERE id = ?", id)
			} else {
				res, err = db.Exec("DELETE FROM links WHERE id LIKE ?", id+"-%")
			}
			if err != nil {
				return affected, err
			}
			n, _ := res.RowsAffected()
			affected += n
		}
	}
	_, _ = db.Exec("VACUUM")
	return affected, nil
}

// CountLinks 统计链接总数。
func CountLinks() int64 {
	db, err := open()
	if err != nil {
		return 0
	}
	defer db.Close()
	var n int64
	_ = db.QueryRow("SELECT COUNT(*) FROM links").Scan(&n)
	return n
}

// ---------------------------------------------------------------- 接口库

const sitesDDL = `
CREATE TABLE IF NOT EXISTS sites (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  domain        TEXT NOT NULL,
  pattern       TEXT NOT NULL,
  method        TEXT,
  endpoint      TEXT,
  body_template TEXT,
  cookie_keys   TEXT,
  extract       TEXT,
  note          TEXT,
  status        TEXT DEFAULT 'active',
  ok_count      INTEGER DEFAULT 0,
  fail_count    INTEGER DEFAULT 0,
  created_at    TEXT DEFAULT CURRENT_TIMESTAMP,
  last_ok       TEXT,
  UNIQUE(domain, pattern)
)`

// Site 接口库记录。
type Site struct {
	ID           int64  `json:"id"`
	Domain       string `json:"domain"`
	Pattern      string `json:"pattern"`
	Method       string `json:"method"`
	Endpoint     string `json:"endpoint"`
	BodyTemplate string `json:"body_template"`
	CookieKeys   string `json:"cookie_keys"`
	Extract      string `json:"extract"`
	Note         string `json:"note"`
	Status       string `json:"status"`
	OKCount      int64  `json:"ok_count"`
	FailCount    int64  `json:"fail_count"`
	CreatedAt    string `json:"created_at"`
	LastOK       string `json:"last_ok"`
}

func sitesConn() (*sql.DB, error) {
	db, err := open()
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(sitesDDL); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// SaveSite 记录（或更新）一个数据接口。
func SaveSite(domain, pattern, method, endpoint, bodyTemplate string, cookieKeys []string, extract, note string) error {
	keys, _ := json.Marshal(cookieKeys)
	if cookieKeys == nil {
		keys = []byte("[]")
	}
	if len(bodyTemplate) > 4000 {
		bodyTemplate = bodyTemplate[:4000]
	}
	db, err := sitesConn()
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`
		INSERT INTO sites (domain, pattern, method, endpoint, body_template, cookie_keys, extract, note)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(domain, pattern) DO UPDATE SET
			method=excluded.method, endpoint=excluded.endpoint,
			body_template=excluded.body_template, cookie_keys=excluded.cookie_keys,
			extract=excluded.extract, note=excluded.note, status='active'`,
		domain, pattern, method, endpoint, bodyTemplate, string(keys), extract, note)
	return err
}

const siteCols = "id, domain, pattern, method, endpoint, body_template, cookie_keys, extract, note, status, ok_count, fail_count, created_at, COALESCE(last_ok,'')"

func scanSite(s interface{ Scan(...any) error }) (Site, error) {
	var v Site
	err := s.Scan(&v.ID, &v.Domain, &v.Pattern, &v.Method, &v.Endpoint, &v.BodyTemplate,
		&v.CookieKeys, &v.Extract, &v.Note, &v.Status, &v.OKCount, &v.FailCount, &v.CreatedAt, &v.LastOK)
	return v, err
}

// FindSite 按域名 + 路径子串找最匹配的接口记录。
func FindSite(rawURL string) *Site {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	db, err := sitesConn()
	if err != nil {
		return nil
	}
	defer db.Close()
	rows, err := db.Query("SELECT "+siteCols+" FROM sites WHERE domain=? AND status='active'", u.Host)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var best *Site
	for rows.Next() {
		s, err := scanSite(rows)
		if err != nil {
			continue
		}
		if s.Pattern != "" && strings.Contains(rawURL, s.Pattern) {
			if best == nil || len(s.Pattern) > len(best.Pattern) {
				cp := s
				best = &cp
			}
		}
	}
	return best
}

// ListSites 列出接口库，domain 为空表示全部。
func ListSites(domain string) []Site {
	db, err := sitesConn()
	if err != nil {
		return nil
	}
	defer db.Close()
	var rows *sql.Rows
	if domain != "" {
		rows, err = db.Query("SELECT "+siteCols+" FROM sites WHERE domain LIKE ? ORDER BY status, domain", "%"+domain+"%")
	} else {
		rows, err = db.Query("SELECT " + siteCols + " FROM sites ORDER BY domain, pattern")
	}
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Site
	for rows.Next() {
		if s, err := scanSite(rows); err == nil {
			out = append(out, s)
		}
	}
	return out
}

// TouchSite 记录接口的成败次数（连续 3 次失败标 stale）。
func TouchSite(rawURL string, ok bool) {
	s := FindSite(rawURL)
	if s == nil {
		return
	}
	db, err := sitesConn()
	if err != nil {
		return
	}
	defer db.Close()
	if ok {
		_, _ = db.Exec("UPDATE sites SET ok_count=ok_count+1, last_ok=CURRENT_TIMESTAMP WHERE id=?", s.ID)
	} else {
		_, _ = db.Exec(`UPDATE sites SET fail_count=fail_count+1,
			status=CASE WHEN fail_count+1>=3 THEN 'stale' ELSE status END WHERE id=?`, s.ID)
	}
}
