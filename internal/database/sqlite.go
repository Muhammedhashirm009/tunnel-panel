package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

// Init opens the SQLite database and creates tables
func Init(dbPath string) error {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return fmt.Errorf("failed to create db directory: %w", err)
	}

	var err error
	db, err = sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Enable WAL mode and foreign keys
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return err
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		return err
	}

	return migrate()
}

// DB returns the global database connection
func DB() *sql.DB {
	return db
}

// Close closes the database connection
func Close() error {
	if db != nil {
		return db.Close()
	}
	return nil
}

// migrate creates all tables if they don't exist
func migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		is_admin INTEGER DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_login DATETIME
	);

	CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS cloudflare_config (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		api_token TEXT NOT NULL DEFAULT '',
		account_id TEXT NOT NULL DEFAULT '',
		zone_id TEXT NOT NULL DEFAULT '',
		zone_name TEXT NOT NULL DEFAULT '',
		tunnel_panel_id TEXT DEFAULT '',
		tunnel_panel_name TEXT DEFAULT '',
		tunnel_apps_id TEXT DEFAULT '',
		tunnel_apps_name TEXT DEFAULT '',
		panel_domain TEXT DEFAULT '',
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS sites (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		domain TEXT UNIQUE NOT NULL,
		document_root TEXT NOT NULL,
		php_version TEXT DEFAULT '8.2',
		port INTEGER UNIQUE NOT NULL,
		nginx_config_path TEXT DEFAULT '',
		dns_record_id TEXT DEFAULT '',
		status TEXT DEFAULT 'active' CHECK(status IN ('active','stopped','error')),
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS docker_apps (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		container_id TEXT DEFAULT '',
		image TEXT NOT NULL,
		domain TEXT DEFAULT '',
		internal_port INTEGER DEFAULT 0,
		mapped_port INTEGER DEFAULT 0,
		dns_record_id TEXT DEFAULT '',
		status TEXT DEFAULT 'created' CHECK(status IN ('running','stopped','created','error')),
		env_vars TEXT DEFAULT '{}',
		volumes TEXT DEFAULT '[]',
		compose_file TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS ports (
		port INTEGER PRIMARY KEY,
		app_type TEXT NOT NULL CHECK(app_type IN ('site','docker','system')),
		app_id INTEGER NOT NULL,
		allocated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS databases_managed (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT UNIQUE NOT NULL,
		db_type TEXT DEFAULT 'mysql' CHECK(db_type IN ('mysql','mariadb','postgres')),
		db_user TEXT NOT NULL,
		db_password_enc TEXT NOT NULL,
		site_id INTEGER DEFAULT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (site_id) REFERENCES sites(id) ON DELETE SET NULL
	);

	CREATE TABLE IF NOT EXISTS audit_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER,
		action TEXT NOT NULL,
		details TEXT DEFAULT '',
		ip_address TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL
	);

	CREATE TABLE IF NOT EXISTS tunnel_ingress_rules (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		domain TEXT UNIQUE NOT NULL,
		target TEXT NOT NULL,
		app_type TEXT NOT NULL CHECK(app_type IN ('site','docker','custom')),
		app_id INTEGER DEFAULT 0,
		enabled INTEGER DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Insert default cloudflare config row if not exists
	INSERT OR IGNORE INTO cloudflare_config (id) VALUES (1);

	-- Insert setup_complete setting if not exists
	INSERT OR IGNORE INTO settings (key, value) VALUES ('setup_complete', 'false');
	`

	_, err := db.Exec(schema)
	return err
}
