package main
import ( "database/sql"; "fmt"; "log"; _ "modernc.org/sqlite" )
func main() {
	db, err := sql.Open("sqlite", "data/web-tr.db")
	if err != nil { log.Fatal(err) }
	defer db.Close()
	var token string
	err = db.QueryRow("SELECT public_token FROM users WHERE public_token != '' LIMIT 1").Scan(&token)
	if err != nil { log.Fatal(err) }
	fmt.Println(token)
}
