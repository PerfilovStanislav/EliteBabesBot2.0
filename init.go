package main

import (
	"EliteBabesBot2.0/shared"
	"fmt"
	"github.com/jmoiron/sqlx"
	"os"
)

func init() {
	shared.LoadEnv()
}

func initDb(dbName string) {
	var err error
	db, err = sqlx.Connect("postgres", fmt.Sprintf("host=%s user=%s password=%s dbname=%s "+
		"sslmode=disable port=%s", os.Getenv("DB_HOST"), os.Getenv("DB_USER"), os.Getenv("DB_PASS"),
		dbName, os.Getenv("DB_PORT")))
	if err != nil {
		panic(err)
	}
}
