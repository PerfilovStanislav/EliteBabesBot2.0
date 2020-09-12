package shared

import (
	"fmt"
	"github.com/fatih/color"
	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
	"github.com/mitchellh/go-ps"
	"os"
)

func LoadEnv() {
	if err := godotenv.Load(); err != nil {
		color.Red("No .env file found")
	}
}

func ConnectToDb() *sqlx.DB {
	db, err := sqlx.Connect("postgres", fmt.Sprintf("host=%s user=%s password=%s dbname=%s "+
		"sslmode=disable port=%s", os.Getenv("DB_HOST"), os.Getenv("DB_USER"), os.Getenv("DB_PASS"),
		os.Getenv("DB_NAME"), os.Getenv("DB_PORT")))
	if err != nil {
		panic(err)
	}
	return db
}

func SingleProcess(name string) {
	processList, err := ps.Processes()
	if err != nil {
		panic(err)
	}

	var count = 0
	for x := range processList {
		var process ps.Process
		process = processList[x]
		if process.Executable() == name {
			count++
		}
	}

	if count > 1 {
		os.Exit(0)
	}
}
