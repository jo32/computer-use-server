package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"time"
)

// Export uses a separate WAL reader and a consistent snapshot so a paused
// export neither blocks new calls nor duplicates rows when history changes.
func (s *Store) Export(ctx context.Context, f Filter, begin func(int), dst io.Writer) error {
	db, err := sql.Open("sqlite", filepath.Join(s.Dir, "history.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	where, args := []string{"1=1"}, []any{}
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
	var total int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM calls WHERE "+clause, args...).Scan(&total); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT * FROM calls WHERE "+clause+" ORDER BY started DESC,rowid DESC", args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	begin(total)
	enc := json.NewEncoder(dst)
	for rows.Next() {
		var c Call
		var started, a, r string
		if err = rows.Scan(&c.ID, &c.Session, &c.Client, &c.Tool, &c.Category, &c.Status, &started, &c.Duration, &a, &r, &c.Error, &c.Screenshot); err != nil {
			return err
		}
		c.Started, _ = time.Parse(time.RFC3339Nano, started)
		c.Arguments = json.RawMessage(a)
		c.Result = json.RawMessage(r)
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = enc.Encode(c); err != nil {
			return err
		}
	}
	return rows.Err()
}
