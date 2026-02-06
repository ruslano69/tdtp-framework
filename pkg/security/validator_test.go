package security

import (
	"strings"
	"testing"
)

func TestNewSQLValidator(t *testing.T) {
	tests := []struct {
		name     string
		safeMode bool
		want     bool
	}{
		{"Safe mode enabled", true, true},
		{"Unsafe mode enabled", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewSQLValidator(tt.safeMode)
			if v.IsSafeMode() != tt.want {
				t.Errorf("NewSQLValidator(%v).IsSafeMode() = %v, want %v",
					tt.safeMode, v.IsSafeMode(), tt.want)
			}
		})
	}
}

func TestSQLValidator_ValidateSafeMode(t *testing.T) {
	validator := NewSQLValidator(true) // Safe mode

	tests := []struct {
		name    string
		sql     string
		wantErr bool
		errMsg  string
	}{
		// Разрешенные запросы
		{
			name:    "Simple SELECT",
			sql:     "SELECT * FROM users",
			wantErr: false,
		},
		{
			name:    "SELECT with WHERE",
			sql:     "SELECT id, name FROM users WHERE age > 18",
			wantErr: false,
		},
		{
			name:    "SELECT with JOIN",
			sql:     "SELECT u.name, o.total FROM users u JOIN orders o ON u.id = o.user_id",
			wantErr: false,
		},
		{
			name:    "SELECT with semicolon at end",
			sql:     "SELECT * FROM users;",
			wantErr: false,
		},
		{
			name:    "WITH (CTE) query",
			sql:     "WITH cte AS (SELECT * FROM users) SELECT * FROM cte",
			wantErr: false,
		},
		{
			name:    "SELECT with lowercase",
			sql:     "select * from users",
			wantErr: false,
		},

		// Запрещенные операции
		{
			name:    "INSERT",
			sql:     "INSERT INTO users (name) VALUES ('test')",
			wantErr: true,
			errMsg:  "only SELECT and WITH",
		},
		{
			name:    "UPDATE",
			sql:     "UPDATE users SET name = 'test' WHERE id = 1",
			wantErr: true,
			errMsg:  "only SELECT and WITH",
		},
		{
			name:    "DELETE",
			sql:     "DELETE FROM users WHERE id = 1",
			wantErr: true,
			errMsg:  "only SELECT and WITH",
		},
		{
			name:    "DROP TABLE",
			sql:     "DROP TABLE users",
			wantErr: true,
			errMsg:  "only SELECT and WITH",
		},
		{
			name:    "CREATE TABLE",
			sql:     "CREATE TABLE test (id INT)",
			wantErr: true,
			errMsg:  "only SELECT and WITH",
		},

		// Запрещенные ключевые слова в SELECT
		{
			name:    "SELECT with DROP",
			sql:     "SELECT * FROM users; DROP TABLE users",
			wantErr: true,
			errMsg:  "forbidden keyword",
		},
		{
			name:    "SELECT with PRAGMA",
			sql:     "SELECT * FROM users WHERE 1=1; PRAGMA table_info(users)",
			wantErr: true,
			errMsg:  "forbidden keyword",
		},

		// Комментарии
		{
			name:    "Single line comment",
			sql:     "SELECT * FROM users -- this is a comment",
			wantErr: true,
			errMsg:  "comments",
		},
		{
			name:    "Multi line comment",
			sql:     "SELECT * FROM users /* comment */",
			wantErr: true,
			errMsg:  "comments",
		},

		// Множественные команды
		{
			name:    "Multiple SELECT statements",
			sql:     "SELECT * FROM users; SELECT * FROM orders",
			wantErr: true,
			errMsg:  "semicolon", // Catches semicolon in middle error
		},
		{
			name:    "Semicolon in middle",
			sql:     "SELECT * FROM users; WHERE id = 1",
			wantErr: true,
			errMsg:  "semicolon allowed only at the end",
		},

		// Edge cases
		{
			name:    "Empty query",
			sql:     "",
			wantErr: true,
		},
		{
			name:    "Whitespace only",
			sql:     "   \n\t  ",
			wantErr: true,
		},
		{
			name:    "Field named DELETED_AT (should pass)",
			sql:     "SELECT deleted_at FROM users",
			wantErr: false,
		},
		{
			name:    "SELECTED as alias (should pass)",
			sql:     "SELECT COUNT(*) as selected FROM users",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.Validate(tt.sql)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() should return error for SQL: %s", tt.sql)
					return
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, should contain %q", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error for SQL %q: %v", tt.sql, err)
				}
			}
		})
	}
}

func TestSQLValidator_ValidateUnsafeMode(t *testing.T) {
	validator := NewSQLValidator(false) // Unsafe mode

	tests := []struct {
		name string
		sql  string
	}{
		{"SELECT", "SELECT * FROM users"},
		{"INSERT", "INSERT INTO users (name) VALUES ('test')"},
		{"UPDATE", "UPDATE users SET name = 'test'"},
		{"DELETE", "DELETE FROM users WHERE id = 1"},
		{"DROP", "DROP TABLE users"},
		{"CREATE", "CREATE TABLE test (id INT)"},
		{"TRUNCATE", "TRUNCATE TABLE users"},
		{"Multiple statements", "DELETE FROM users; DROP TABLE users;"},
		{"With comments", "SELECT * FROM users -- comment"},
		{"PRAGMA", "PRAGMA table_info(users)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.Validate(tt.sql)
			if err != nil {
				t.Errorf("Validate() in unsafe mode should allow all queries, got error: %v", err)
			}
		})
	}
}

func TestSQLValidator_SetSafeMode(t *testing.T) {
	validator := NewSQLValidator(true)

	// Проверяем начальное состояние
	if !validator.IsSafeMode() {
		t.Error("Initial safe mode should be true")
	}

	// Меняем на unsafe
	validator.SetSafeMode(false)
	if validator.IsSafeMode() {
		t.Error("Safe mode should be false after SetSafeMode(false)")
	}

	// Меняем обратно на safe
	validator.SetSafeMode(true)
	if !validator.IsSafeMode() {
		t.Error("Safe mode should be true after SetSafeMode(true)")
	}
}

func TestCheckForbiddenKeywords(t *testing.T) {
	validator := NewSQLValidator(true)

	tests := []struct {
		name    string
		sql     string
		wantErr bool
	}{
		{"No forbidden keywords", "SELECT ID, NAME FROM USERS", false},
		{"INSERT keyword", "SELECT * FROM USERS WHERE STATUS = 'INSERT'", false}, // 'INSERT' as value
		{"DELETE in field name", "SELECT DELETED_AT FROM USERS", false},          // 'DELETE' in column name
		{"Actual DELETE", "SELECT * FROM USERS DELETE FROM ORDERS", true},
		{"DROP keyword", "SELECT * FROM USERS DROP TABLE TEMP", true},
		{"TRUNCATE keyword", "SELECT * TRUNCATE TABLE USERS", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.checkForbiddenKeywords(strings.ToUpper(tt.sql))
			if (err != nil) != tt.wantErr {
				t.Errorf("checkForbiddenKeywords() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCheckMultipleStatements(t *testing.T) {
	validator := NewSQLValidator(true)

	tests := []struct {
		name    string
		sql     string
		wantErr bool
	}{
		{"No semicolon", "SELECT * FROM users", false},
		{"Semicolon at end", "SELECT * FROM users;", false},
		{"Multiple statements", "SELECT * FROM users; SELECT * FROM orders", true},
		{"Semicolon in middle", "SELECT * FROM users; WHERE id = 1", true},
		{"Multiple semicolons", ";;;", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.checkMultipleStatements(tt.sql)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkMultipleStatements() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCheckComments(t *testing.T) {
	validator := NewSQLValidator(true)

	tests := []struct {
		name    string
		sql     string
		wantErr bool
	}{
		{"No comments", "SELECT * FROM users", false},
		{"Single line comment", "SELECT * FROM users -- comment", true},
		{"Multi line comment start", "SELECT * FROM users /* comment", true},
		{"Multi line comment end", "SELECT * FROM users */", true},
		{"Both comment types", "SELECT * FROM users -- test /* test */", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.checkComments(tt.sql)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkComments() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetQueryType(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{"SELECT", "SELECT * FROM users", "SELECT"},
		{"INSERT", "INSERT INTO users VALUES (1)", "INSERT"},
		{"UPDATE", "UPDATE users SET name = 'test'", "UPDATE"},
		{"DELETE", "DELETE FROM users", "DELETE"},
		{"WITH", "WITH cte AS (...) SELECT * FROM cte", "WITH"},
		{"Empty", "", "UNKNOWN"},
		{"Whitespace", "   ", "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getQueryType(strings.ToUpper(tt.sql))
			if got != tt.want {
				t.Errorf("getQueryType() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Benchmark тесты
func BenchmarkValidate_SimpleSelect(b *testing.B) {
	validator := NewSQLValidator(true)
	sql := "SELECT id, name, email FROM users WHERE age > 18 ORDER BY created_at DESC"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validator.Validate(sql)
	}
}

func BenchmarkValidate_ComplexQuery(b *testing.B) {
	validator := NewSQLValidator(true)
	sql := `
		WITH active_users AS (
			SELECT id, name FROM users WHERE is_active = 1
		)
		SELECT u.id, u.name, COUNT(o.id) as order_count
		FROM active_users u
		LEFT JOIN orders o ON u.id = o.user_id
		GROUP BY u.id, u.name
		HAVING order_count > 10
		ORDER BY order_count DESC
		LIMIT 100
	`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validator.Validate(sql)
	}
}

func BenchmarkValidate_UnsafeMode(b *testing.B) {
	validator := NewSQLValidator(false)
	sql := "DROP TABLE users; DELETE FROM orders; TRUNCATE sessions;"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validator.Validate(sql)
	}
}
