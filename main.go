package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"
	"time"
)

type mockDriver struct{}

func (d *mockDriver) Open(name string) (driver.Conn, error) {
	return &mockConn{},
		nil
}

type mockConn struct {
	mu     sync.Mutex
	isBad  bool
	closed bool
}

func (c *mockConn) Prepare(query string) (driver.Stmt, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.isBad || c.closed {
		return nil, driver.ErrBadConn
	}
	return &mockStmt{conn: c, query: query}, nil
}

func (c *mockConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func (c *mockConn) Begin() (driver.Tx, error) {
	return nil, errors.New("not implemented")
}

func (c *mockConn) ResetSession(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.isBad {
		return driver.ErrBadConn
	}
	return nil
}

type mockStmt struct {
	conn  *mockConn
	query string
}

func (s *mockStmt) Close() error {
	return nil
}

func (s *mockStmt) NumInput() int {
	return 0
}

func (s *mockStmt) Exec(args []driver.Value) (driver.Result, error) {
	return nil, errors.New("use ExecContext")
}

func (s *mockStmt) Query(args []driver.Value) (driver.Rows, error) {
	return nil, errors.New("use QueryContext")
}

func (s *mockStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	s.conn.mu.Lock()
	if s.conn.isBad || s.conn.closed {
		s.conn.mu.Unlock()
		return nil, driver.ErrBadConn
	}
	s.conn.mu.Unlock()

	// Simulate long-running query if SELECT SLEEP
	if s.query == "SELECT SLEEP(2)" {
		select {
		case <-time.After(2 * time.Second):
			return &mockRows{rows: 1}, nil
		case <-ctx.Done():
			s.conn.mu.Lock()
			s.conn.isBad = true
			s.conn.mu.Unlock()
			return nil, driver.ErrBadConn
		}
	}

	return &mockRows{rows: 1}, nil
}

func (s *mockStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	s.conn.mu.Lock()
	if s.conn.isBad || s.conn.closed {
		s.conn.mu.Unlock()
		return nil, driver.ErrBadConn
	}
	s.conn.mu.Unlock()

	select {
	case <-ctx.Done():
		s.conn.mu.Lock()
		s.conn.isBad = true
		s.conn.mu.Unlock()
		return nil, driver.ErrBadConn
	default:
	}

	return &mockResult{}, nil
}

type mockRows struct {
	rows int
}

func (r *mockRows) Columns() []string {
	return []string{"col"}
}

func (r *mockRows) Close() error {
	return nil
}

func (r *mockRows) Next(dest []driver.Value) error {
	if r.rows <= 0 {
		return errors.New("EOF")
	}
	r.rows--
	dest[0] = 1
	return nil
}

type mockResult struct{CustomResult interface{}}

func (r *mockResult) LastInsertId() (int64, error) {
	return 0, nil
}

func (r *mockResult) RowsAffected() (int64, error) {
	return 1, nil
}

func init() {
	sql.Register("mockmysql", &mockDriver{})
}

func main() {
	db, err := sql.Open("mockmysql", "dsn")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	db.SetMaxIdleConns(1)
	db.SetMaxOpenConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err = db.QueryContext(ctx, "SELECT SLEEP(2)")
	if err == nil {
		panic("expected timeout error")
	}

	var val int
	err = db.QueryRow("SELECT 1").Scan(&val)
	if err != nil {
		panic(fmt.Sprintf("second query failed: %v", err))
	}

	fmt.Println("Success!")
}