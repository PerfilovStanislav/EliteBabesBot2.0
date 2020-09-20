package shared

import (
	"fmt"
	"github.com/fatih/color"
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
	} else if count == 0 {
		panic(processList)
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
			return b.BotAPI.Send(c)
		}
	}
	return resp, err
}

func (b *Bot) ReSendMediaGroup(c tgbotapi.MediaGroupConfig) ([]tgbotapi.Message, error) {
	var resp, err = b.SendMediaGroup(c)
	if err != nil {
		fmt.Println(err)
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
