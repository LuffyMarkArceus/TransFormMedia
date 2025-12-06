package neondb

import (
	"database/sql"
	"log"

	_ "github.com/lib/pq"
)

func Connect(url string) *sql.DB {
	db, err := sql.Open("postgres", url)
	if err != nil {
		log.Fatal(err)
	}
	return db
}
