package shared

import (
	"fmt"
	"github.com/fatih/color"
	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
	"github.com/mitchellh/go-ps"
	"os"
	tgbotapi "telegram-bot-api"
	"time"
)

type Bot struct {
	*tgbotapi.BotAPI
}

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

func NewBot(token string) *Bot {
	bot, _ := tgbotapi.NewBotAPI(token)
	return &Bot{
		BotAPI: bot,
	}
}

func (b *Bot) ReSend(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	var resp, err = b.BotAPI.Send(c)
	if err != nil {
		var botError = err.(*tgbotapi.Error)
		if botError.RetryAfter > 0 {
			time.Sleep(time.Second * (time.Duration(botError.RetryAfter) + 1))
			return b.ReSend(c)
		} else {
			time.Sleep(time.Second * time.Duration(10))
			return b.Send(c)
		}
	}
	return resp, err
}

func (b *Bot) ReSendMediaGroup(c tgbotapi.MediaGroupConfig) ([]tgbotapi.Message, error) {
	var resp, err = b.SendMediaGroup(c)
	if err != nil {
		var botError = err.(*tgbotapi.Error)
		if botError.RetryAfter > 0 {
			time.Sleep(time.Second * (time.Duration(botError.RetryAfter) + 1))
			return b.ReSendMediaGroup(c)
		} else {
			time.Sleep(time.Second * time.Duration(10))
			return b.SendMediaGroup(c)
		}
	}
	return resp, err
}
