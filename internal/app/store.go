package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strings"
	"time"
)

// Schema version 1. Use additive, versioned migrations for future schema changes.
func Migrate(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS conversion_history (
	 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	 session_hash CHAR(64) NOT NULL,
	 content_hash CHAR(64) NOT NULL,
	 title VARCHAR(160) NOT NULL,
	 input MEDIUMTEXT NOT NULL,
	 indent_style VARCHAR(8) NOT NULL,
	 mode VARCHAR(16) NOT NULL,
	 input_bytes INT NOT NULL,
	 updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	 UNIQUE KEY unique_conversion (session_hash, content_hash),
	 KEY history_by_session (session_hash, updated_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin`)
	return err
}

type History struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Input     string    `json:"input,omitempty"`
	Indent    string    `json:"indent"`
	Mode      string    `json:"mode"`
	Bytes     int       `json:"bytes"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func hash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func (a *App) save(ctx context.Context, session string, req FormatRequest) error {
	title := []rune(strings.Join(strings.Fields(req.Input), " "))
	if len(title) > 90 {
		title = append(title[:90], '…')
	}
	_, err := a.db.ExecContext(ctx, `INSERT INTO conversion_history (session_hash, content_hash, title, input, indent_style, mode, input_bytes) VALUES (?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE updated_at=CURRENT_TIMESTAMP(6)`, session, hash(req.Input+"\x00"+req.Indent+"\x00"+req.Mode), string(title), req.Input, req.Indent, req.Mode, len(req.Input))
	return err
}

func (a *App) list(ctx context.Context, session string) ([]History, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT id,title,indent_style,mode,input_bytes,updated_at FROM conversion_history WHERE session_hash=? ORDER BY updated_at DESC,id DESC LIMIT 100`, session)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]History, 0)
	for rows.Next() {
		var h History
		if err := rows.Scan(&h.ID, &h.Title, &h.Indent, &h.Mode, &h.Bytes, &h.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, h)
	}
	return items, rows.Err()
}
