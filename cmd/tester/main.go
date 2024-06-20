package main

import (
	"database/sql"
	"github.com/pkg/errors"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func main() {
	db, err := sql.Open("mysql", "testing:testing@tcp(icinga-kubernetes-testing-database-service:3306)/testing")
	if err != nil {
		log.Fatal(errors.Wrap(err, "can't connect to database"))
	}
	defer db.Close()

	for {
		rows, err := db.Query("SELECT * FROM pod")
		if err != nil {
			log.Fatal(errors.Wrap(err, "can't execute query"))
		}

		var uuid []byte
		var namespace, name string

		for rows.Next() {
			err = rows.Scan(&uuid, &namespace, &name)
			if err != nil {
				log.Fatal(errors.Wrap(err, "can't scan row"))
			}
			log.Println(uuid, namespace, name)
		}

		log.Println("Sleeping for 10 seconds")

		time.Sleep(20 * time.Second)
	}
}
