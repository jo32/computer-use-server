// Package store keeps durable tool-call history. Screenshots live separately from JSON.
package store

import (
	"database/sql"
	"encoding/json"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Call struct {
	ID         string          `json:"id"`
	Session    string          `json:"session"`
	Client     string          `json:"client"`
	Tool       string          `json:"tool"`
	Category   string          `json:"category"`
	Status     string          `json:"status"`
	Started    time.Time       `json:"started"`
	Duration   int64           `json:"duration_ms"`
	Arguments  json.RawMessage `json:"arguments"`
	Result     json.RawMessage `json:"result"`
	Error      string          `json:"error,omitempty"`
	Screenshot string          `json:"screenshot,omitempty"`
}
type Filter struct {
	Query, Session, Category, Status string
	Limit, Offset                    int
	Lightweight                      bool
}
type Store struct {
	db  *sql.DB
	Dir string
}

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dir, "screenshots"), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "history.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
 CREATE TABLE IF NOT EXISTS calls (id TEXT PRIMARY KEY, session TEXT NOT NULL, client TEXT NOT NULL, tool TEXT NOT NULL, category TEXT NOT NULL, status TEXT NOT NULL, started TEXT NOT NULL, duration INTEGER NOT NULL, arguments TEXT NOT NULL, result TEXT NOT NULL, error TEXT NOT NULL, screenshot TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS calls_started ON calls(started DESC);
 CREATE INDEX IF NOT EXISTS calls_session ON calls(session);
 UPDATE calls SET status='interrupted', error='服务重启，调用未完成' WHERE status='running';`)
	if err != nil {
		db.Close()
		return nil, err
	}
	_ = os.Chmod(filepath.Join(dir, "history.db"), 0600)
	return &Store{db: db, Dir: dir}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Save(c Call) error {
	if len(c.Arguments) == 0 {
		c.Arguments = json.RawMessage(`{}`)
	}
	if len(c.Result) == 0 {
		c.Result = json.RawMessage(`null`)
	}
	_, err := s.db.Exec(`INSERT INTO calls VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,duration=excluded.duration,result=excluded.result,error=excluded.error,screenshot=excluded.screenshot`, c.ID, c.Session, c.Client, c.Tool, c.Category, c.Status, c.Started.UTC().Format(time.RFC3339Nano), c.Duration, string(c.Arguments), string(c.Result), c.Error, c.Screenshot)
	return err
}
func (s *Store) List(f Filter) ([]Call, int, error) {
	where := []string{"1=1"}
	args := []any{}
	for _, p := range [][2]string{{"session", f.Session}, {"category", f.Category}, {"status", f.Status}} {
		if p[1] != "" {
			where = append(where, p[0]+" = ?")
			args = append(args, p[1])
		}
	}
	if f.Query != "" {
		where = append(where, "(tool LIKE ? OR client LIKE ? OR arguments LIKE ? OR error LIKE ?)")
		for i := 0; i < 4; i++ {
			args = append(args, "%"+f.Query+"%")
		}
	}
	clause := strings.Join(where, " AND ")
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM calls WHERE "+clause, args...).Scan(&count); err != nil {
		return nil, 0, err
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	projection := "*"
	if f.Lightweight {
		projection = `id,session,client,tool,category,status,started,duration,
  json_remove(arguments,'$.content','$.env','$.text'),
  json_object('running',json_extract(result,'$.running'),'exit_code',json_extract(result,'$.exit_code'),'frame_id',json_extract(result,'$.frame_id'),'image_size',json_extract(result,'$.image_size')),error,screenshot`
	}
	rows, err := s.db.Query("SELECT "+projection+" FROM calls WHERE "+clause+" ORDER BY started DESC,rowid DESC LIMIT ? OFFSET ?", append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Call{}
	for rows.Next() {
		var c Call
		var started, a, r string
		if err := rows.Scan(&c.ID, &c.Session, &c.Client, &c.Tool, &c.Category, &c.Status, &started, &c.Duration, &a, &r, &c.Error, &c.Screenshot); err != nil {
			return nil, 0, err
		}
		c.Started, _ = time.Parse(time.RFC3339Nano, started)
		c.Arguments = json.RawMessage(a)
		c.Result = json.RawMessage(r)
		out = append(out, c)
	}
	return out, count, rows.Err()
}
func (s *Store) Summary() (map[string]any, error) {
	var total, failed, running, screens int
	var avg float64
	err := s.db.QueryRow(`SELECT count(*),coalesce(sum(status IN ('error','denied','interrupted')),0),coalesce(sum(status='running'),0),coalesce(sum(screenshot!=''),0),coalesce(avg(CASE WHEN status!='running' THEN duration END),0) FROM calls`).Scan(&total, &failed, &running, &screens, &avg)
	return map[string]any{"total": total, "failed": failed, "running": running, "screenshots": screens, "avg_ms": int(avg)}, err
}
func (s *Store) Sessions() ([]map[string]any, error) {
	rows, err := s.db.Query(`SELECT session,client,count(*),max(started),sum(status='running') FROM calls GROUP BY session,client ORDER BY max(started) DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, client, last string
		var n, r int
		if err := rows.Scan(&id, &client, &n, &last, &r); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "client": client, "count": n, "last": last, "running": r})
	}
	return out, rows.Err()
}

// Get returns the complete durable record, independently of list pagination.
func (s *Store) Get(id string) (Call, error) {
	return scanCall(s.db.QueryRow("SELECT * FROM calls WHERE id=?", id))
}
func (s *Store) Frame(session, id string) (Call, error) {
	return scanCall(s.db.QueryRow("SELECT * FROM calls WHERE session=? AND screenshot!='' AND json_extract(result,'$.frame_id')=? ORDER BY started DESC,rowid DESC LIMIT 1", session, id))
}

type scanner interface{ Scan(...any) error }

func scanCall(row scanner) (Call, error) {
	var c Call
	var started, a, r string
	err := row.Scan(&c.ID, &c.Session, &c.Client, &c.Tool, &c.Category, &c.Status, &started, &c.Duration, &a, &r, &c.Error, &c.Screenshot)
	if err != nil {
		return c, err
	}
	c.Started, _ = time.Parse(time.RFC3339Nano, started)
	c.Arguments = json.RawMessage(a)
	c.Result = json.RawMessage(r)
	return c, nil
}
