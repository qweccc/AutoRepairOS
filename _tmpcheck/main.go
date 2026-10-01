package main
import ("database/sql";"fmt";_ "github.com/mattn/go-sqlite3")
func main() {
	db,_:=sql.Open("sqlite3","file:C:/Users/fr/.openclaw/workspace/data/autorepair.db?_journal_mode=WAL&_busy_timeout=5000")
	defer db.Close()
	var n int
	db.QueryRow("SELECT COUNT(*) FROM users").Scan(&n)
	fmt.Println("users count (HOST bind-mounted DB):", n)
	rows,_:=db.Query("SELECT username,role FROM users ORDER BY id")
	defer rows.Close()
	for rows.Next(){var u,role sql.NullString;rows.Scan(&u,&role);fmt.Printf("  %s (%s)\n",u.String,role.String)}
	// also check schema_migrations
	var m int
	db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&m)
	fmt.Println("schema_migrations count:", m)
}
