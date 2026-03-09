package dbmanager

import (
	"fmt"
	"os/exec"
	"strings"
)

// Manager provides MySQL/MariaDB management via CLI
type Manager struct {
	// Socket-auth mode: mysql runs as root via sudo, no password needed
	sudoUser string
}

// NewManager creates a new database manager
func NewManager() *Manager {
	return &Manager{sudoUser: "root"}
}

// Database represents a MySQL database
type Database struct {
	Name   string `json:"name"`
	Size   int64  `json:"size_bytes"`
	Tables int    `json:"tables"`
}

// DBUser represents a MySQL user
type DBUser struct {
	User string `json:"user"`
	Host string `json:"host"`
}

// mysqlExec runs a mysql command as root (socket auth)
func (m *Manager) mysqlExec(query string) (string, error) {
	cmd := exec.Command("mysql",
		"--user=root",
		"--execute="+query,
		"--batch",
		"--skip-column-names",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Try with sudo if direct root fails
		cmd2 := exec.Command("sudo", "mysql",
			"--user=root",
			"--execute="+query,
			"--batch",
			"--skip-column-names",
		)
		out2, err2 := cmd2.CombinedOutput()
		if err2 != nil {
			return string(out2), fmt.Errorf("mysql error: %s", strings.TrimSpace(string(out2)))
		}
		return strings.TrimSpace(string(out2)), nil
	}
	return strings.TrimSpace(string(out)), nil
}

// IsAvailable checks if MySQL/MariaDB CLI is accessible
func (m *Manager) IsAvailable() bool {
	_, err := m.mysqlExec("SELECT 1")
	return err == nil
}

// ListDatabases returns all non-system databases
func (m *Manager) ListDatabases() ([]Database, error) {
	out, err := m.mysqlExec("SHOW DATABASES")
	if err != nil {
		return nil, err
	}

	systemDBs := map[string]bool{
		"information_schema": true,
		"performance_schema": true,
		"sys":                true,
		"mysql":              true,
	}

	var dbs []Database
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if name == "" || systemDBs[name] {
			continue
		}
		db := Database{Name: name}

		// Get size
		sizeQuery := fmt.Sprintf(
			"SELECT COALESCE(SUM(data_length + index_length), 0) FROM information_schema.tables WHERE table_schema = '%s'",
			sanitize(name),
		)
		sizeOut, err := m.mysqlExec(sizeQuery)
		if err == nil {
			fmt.Sscanf(strings.TrimSpace(sizeOut), "%d", &db.Size)
		}

		// Get table count
		tableQuery := fmt.Sprintf(
			"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = '%s'",
			sanitize(name),
		)
		tableOut, err := m.mysqlExec(tableQuery)
		if err == nil {
			fmt.Sscanf(strings.TrimSpace(tableOut), "%d", &db.Tables)
		}

		dbs = append(dbs, db)
	}
	return dbs, nil
}

// CreateDatabase creates a new database
func (m *Manager) CreateDatabase(name string) error {
	if !isValidName(name) {
		return fmt.Errorf("invalid database name: must be alphanumeric with underscores/hyphens only")
	}
	_, err := m.mysqlExec(fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci", name))
	return err
}

// DropDatabase drops a database
func (m *Manager) DropDatabase(name string) error {
	if !isValidName(name) {
		return fmt.Errorf("invalid database name")
	}
	_, err := m.mysqlExec(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", name))
	return err
}

// GetTables returns the tables in a database
func (m *Manager) GetTables(dbName string) ([]string, error) {
	if !isValidName(dbName) {
		return nil, fmt.Errorf("invalid database name")
	}
	out, err := m.mysqlExec(fmt.Sprintf("SHOW TABLES IN `%s`", dbName))
	if err != nil {
		return nil, err
	}
	var tables []string
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		if t != "" {
			tables = append(tables, t)
		}
	}
	return tables, nil
}

// ListUsers returns all non-system MySQL users
func (m *Manager) ListUsers() ([]DBUser, error) {
	out, err := m.mysqlExec("SELECT User, Host FROM mysql.user ORDER BY User, Host")
	if err != nil {
		return nil, err
	}
	systemUsers := map[string]bool{
		"root": true, "mysql.sys": true, "mysql.session": true,
		"mysql.infoschema": true, "debian-sys-maint": true,
	}
	var users []DBUser
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		user, host := parts[0], parts[1]
		if systemUsers[user] {
			continue
		}
		users = append(users, DBUser{User: user, Host: host})
	}
	return users, nil
}

// CreateUser creates a MySQL user and optionally grants access to a database
func (m *Manager) CreateUser(user, password, host, database string) error {
	if host == "" {
		host = "localhost"
	}
	if !isValidName(user) {
		return fmt.Errorf("invalid username")
	}

	createSQL := fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'%s' IDENTIFIED BY '%s'",
		sanitize(user), sanitize(host), sanitize(password))
	if _, err := m.mysqlExec(createSQL); err != nil {
		return err
	}

	if database != "" && isValidName(database) {
		grantSQL := fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'%s'; FLUSH PRIVILEGES",
			sanitize(database), sanitize(user), sanitize(host))
		if _, err := m.mysqlExec(grantSQL); err != nil {
			return err
		}
	} else {
		m.mysqlExec("FLUSH PRIVILEGES")
	}
	return nil
}

// DropUser drops a MySQL user
func (m *Manager) DropUser(user, host string) error {
	if host == "" {
		host = "localhost"
	}
	_, err := m.mysqlExec(fmt.Sprintf("DROP USER IF EXISTS '%s'@'%s'; FLUSH PRIVILEGES",
		sanitize(user), sanitize(host)))
	return err
}

// sanitize removes single quotes to prevent SQL injection in CLI args
func sanitize(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// isValidName validates identifiers to prevent injection
func isValidName(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
