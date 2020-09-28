package main

import (
	"EliteBabesBot2.0/shared"
	"encoding/json"
	"fmt"
	"github.com/antchfx/htmlquery"
	"github.com/disintegration/imaging"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	_ "github.com/lib/pq"
	"golang.org/x/image/draw"
	"golang.org/x/net/html"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"telegram-bot-api"
	"time"
)

const (
	WaterMark       = "watermark.png"
	StatusActive    = 1
	StatusDeleted   = 2
	StatusPublished = 3

	CronActionChangeHour   = 1
	CronActionChangeMinute = 2
	CronActionSetTime      = 3
	CronActionDelete       = 4
)

var (
	bot              *shared.Bot
	db               *sqlx.DB
	imageWatermark   image.Image
	adminGroupId     int64
	publishChannelId int64
	parseStatus      = 0

	refreshCrons = true
)

func main() {
	initDb(os.Getenv("DB_NAME"))
	initWatermark()
	initBot()

	updates := bot.ListenForWebhook(os.Getenv("LocalUrl"))
	go http.ListenAndServe(":"+os.Getenv("localPort"), nil)

	go startCron()

	for update := range updates {
		go processUpdate(update)
	}

}

func initBot() {
	bot = shared.NewBot(os.Getenv("EliteBabesMultiParseBotToken"))
	//bot.Debug = true

	_, _ = bot.Send(tgbotapi.NewWebhook(os.Getenv("WebhookUrl") + os.Getenv("LocalUrl")))

	adminGroupId, _ = strconv.ParseInt(os.Getenv("AdminGroupId"), 10, 64)
	publishChannelId, _ = strconv.ParseInt(os.Getenv("ChannelForPublishId"), 10, 64)

	//bot.Send(tgbotapi.NewSetMyCommands(tgbotapi.BotCommand{
	//	Command:     "del",
	//	Description: "удалить подборку",
	//}, tgbotapi.BotCommand{
	//	Command:     "stop_parsing",
	//	Description: "остановить парсинг сайта",
	//}, tgbotapi.BotCommand{
	//	Command:     "cron",
	//	Description: "планировщик",
	//}, tgbotapi.BotCommand{
	//	Command:     "stat",
	//	Description: "статистика",
	//}, tgbotapi.BotCommand{
	//	Command:     "show_next",
	//	Description: "показать следующий альбом",
	//}, tgbotapi.BotCommand{
	//	Command:     "menu",
	//	Description: "Меню",
	//}, tgbotapi.BotCommand{
	//	Command:     "send",
	//	Description: "Отправить сейчас",
	//}))
}

func initWatermark() {
	fileWatermark, err := os.Open(WaterMark)
	if err != nil {
		log.Fatalf("failed to open: %s", err)
	}
	defer fileWatermark.Close()

	imageWatermark, err = png.Decode(fileWatermark)
	if err != nil {
		log.Fatalf("failed to decode: %s", err)
	}

	imageWatermark = imaging.Resize(imageWatermark, 50, 50, imaging.Lanczos)
}

func startCron() {
	var crons []Cron
	for {
		if refreshCrons {
			crons = []Cron{}
			fillCrons(&crons)
			refreshCrons = false
		}

		now := time.Now()
		for _, cron := range crons {
			if (cron.Hour+21)%24 == now.UTC().Hour() && cron.Minute == now.Minute() {
				work()
			}
		}
		time.Sleep(time.Second * time.Duration(60-time.Now().Second()))
	}
}

func work() {
	link := Link{}
	if db.Get(&link, `
		SELECT id, model
		FROM links
		WHERE status = $1 AND chat_id = $2
		ORDER BY id
		LIMIT 1
	`, StatusActive, adminGroupId) != nil {
		return
	}

	sendPhotos(link)
}

func sendNow(messageId int) {
	link := Link{}
	if db.Get(&link, `		
		SELECT l.id, l.model, l.status
		FROM media _m1
		JOIN links l on _m1.link_id = l.id AND l.chat_id = $1
		WHERE _m1.message_id = $2
	`, adminGroupId, messageId) != nil {
		return
	}

	if link.Status == StatusActive {
		sendPhotos(link)
	} else {
		bot.ReSend(tgbotapi.NewMessage(
			adminGroupId,
			"Не удалось отправить",
		))
	}
}

func sendPhotos(link Link) {
	var medias []Media
	_ = db.Select(&medias, `
		SELECT file_id, message_id FROM media where link_id = $1 order by message_id
	`, link.Id)

	var files []interface{}
	for i, media := range medias {
		inpMedia := tgbotapi.NewInputMediaPhoto(tgbotapi.FileID(media.FileId))
		if i == 0 {
			inpMedia.ParseMode = tgbotapi.ModeMarkdown
			inpMedia.Caption = fmt.Sprintf("%s #%s",
				os.Getenv("Description"), strings.Replace(link.Model, " ", "", -1))
		}
		files = append(files, inpMedia)
	}
	config := tgbotapi.NewMediaGroup(publishChannelId, files)
	_, _ = bot.ReSendMediaGroup(config)

	// Удаляем альбом
	for _, media := range medias {
		deleteMessage := tgbotapi.NewDeleteMessage(adminGroupId, media.MessageId)
		_, _ = bot.Send(deleteMessage)
	}
	_, _ = db.Exec(`UPDATE links SET status = $1 where id = $2`, StatusPublished, link.Id)
}

func fillCrons(crons *[]Cron) {
	_ = db.Select(crons, `
		SELECT
			id,
			extract(hour from time) as hour,
			extract(minute from time) as minute
		FROM cron
		WHERE bot_id = $1
		ORDER BY hour, minute
	`, bot.Self.ID)
}

func processUpdate(update tgbotapi.Update) {
	if update.Message != nil {
		text := update.Message.Text
		if text != "" {
			command := strings.Split(text, "@")[0]
			if isValidUrl(text) {
				parseStatus = 1
				getAlbums(text)
			} else if command == "/stop_parsing" || text == "🏁 stop parsing" {
				parseStatus = 2
			} else if command == "/cron" || text == "⏰ cron" {
				showCron(update.Message.Chat.ID)
			} else if command == "/del" && update.Message.ReplyToMessage != nil {
				delAlbum(update.Message.ReplyToMessage.MessageID, update.Message.MessageID)
			} else if command == "/send" && update.Message.ReplyToMessage != nil {
				sendNow(update.Message.ReplyToMessage.MessageID)
			} else if command == "/stat" || text == "📊 stat" {
				showStat()
			} else if command == "/show_next" || text == "🔜 show next" {
				showNext()
			} else if command == "/menu" {
				showMenu()
			}
		}
	} else if update.CallbackQuery != nil {
		var action Action
		_ = json.Unmarshal([]byte(update.CallbackQuery.Data), &action)
		if action.Action == CronActionChangeHour {
			changeCronHour(update.CallbackQuery.Message, action.Cron)
		} else if action.Action == CronActionChangeMinute {
			changeCronMinute(update.CallbackQuery.Message, action.Cron)
		} else if action.Action == CronActionSetTime {
			setCronTime(update.CallbackQuery.Message, action.Cron)
		} else if action.Action == CronActionDelete {
			deleteCronTime(update.CallbackQuery.Message, action.Cron)
		}
	}
}

func showMenu() {
	config := tgbotapi.NewMessage(adminGroupId, "Меню!")
	config.ReplyMarkup = tgbotapi.NewOneTimeReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🏁 stop parsing"),
			tgbotapi.NewKeyboardButton("⏰ cron"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📊 stat"),
			tgbotapi.NewKeyboardButton("🔜 show next"),
		),
	)
	_, _ = bot.ReSend(config)
}

func showNext() {
	media := Media{}
	if db.Get(&media, `
		WITH _l AS (
			SELECT l.id
			FROM links l
			WHERE l.status = $1 AND l.chat_id = $2
			ORDER BY l.id
			LIMIT 1
		), _m AS (
			SELECT min(message_id) as message_id, max(message_id) as max_message_id
			FROM media m
			JOIN _l on m.link_id = _l.id
		)
		SELECT _m.message_id, m.file_id
		FROM media m
		JOIN _l ON _l.id = m.link_id
		JOIN _m ON _m.max_message_id = m.message_id
	`, StatusActive, adminGroupId) != nil {
		return
	}

	config := tgbotapi.NewPhoto(adminGroupId, tgbotapi.FileID(media.FileId))
	if media.MessageId != 0 {
		config.ReplyToMessageID = media.MessageId
	}
	_, _ = bot.ReSend(config)
}

func changeCronHour(message *tgbotapi.Message, cron Cron) {
	var keyboard tgbotapi.InlineKeyboardMarkup
	for row := 0; row < 6; row++ {
		var buttons []tgbotapi.InlineKeyboardButton
		for col := 0; col < 4; col++ {
			cron.Hour = row + col*6
			buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("%.2d", cron.Hour),
				cronAction(CronActionChangeMinute, cron),
			))
		}
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, buttons)
	}
	_, _ = bot.ReSend(tgbotapi.NewEditMessageReplyMarkup(
		message.Chat.ID,
		message.MessageID,
		keyboard,
	))
}

func changeCronMinute(message *tgbotapi.Message, cron Cron) {
	var keyboard tgbotapi.InlineKeyboardMarkup
	for row := 0; row < 15; row++ {
		var buttons []tgbotapi.InlineKeyboardButton
		for col := 0; col < 4; col++ {
			cron.Minute = row + col*15
			buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("%.2d:%.2d", cron.Hour, cron.Minute),
				cronAction(CronActionSetTime, cron),
			))
		}
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, buttons)
	}
	_, _ = bot.ReSend(tgbotapi.NewEditMessageReplyMarkup(
		message.Chat.ID,
		message.MessageID,
		keyboard,
	))
}

func setCronTime(message *tgbotapi.Message, cron Cron) {
	if cron.Id > 0 {
		_, _ = db.Exec(`
			UPDATE cron
			SET time = $1
			WHERE id = $2`,
			fmt.Sprintf("%.2d:%.2d:00", cron.Hour, cron.Minute), cron.Id,
		)
	} else {
		_, _ = db.Exec(`
			INSERT INTO cron (time, bot_id) 
			VALUES ($1, $2)`,
			fmt.Sprintf("%.2d:%.2d:00", cron.Hour, cron.Minute), bot.Self.ID,
		)
	}

	config := tgbotapi.NewEditMessageText(
		message.Chat.ID,
		message.MessageID,
		fmt.Sprintf("Время установлено \nМск: *%.2d:%.2d* \nUTC: *%.2d:%.2d*",
			cron.Hour, cron.Minute,
			(cron.Hour+21)%24, cron.Minute),
	)
	config.ParseMode = tgbotapi.ModeMarkdownV2
	_, _ = bot.ReSend(config)

	refreshCrons = true
}

func deleteCronTime(message *tgbotapi.Message, cron Cron) {
	_, _ = db.Exec(`
			DELETE FROM cron
			WHERE id = $1`,
		cron.Id,
	)

	config := tgbotapi.NewEditMessageText(
		message.Chat.ID,
		message.MessageID,
		fmt.Sprintf("Удален cron \nМск: *%.2d:%.2d* \nUTC: *%.2d:%.2d*",
			cron.Hour, cron.Minute,
			(cron.Hour+21)%24, cron.Minute),
	)
	config.ParseMode = tgbotapi.ModeMarkdownV2
	_, _ = bot.ReSend(config)
}

func showCron(chatId int64) {
	var crons []Cron
	fillCrons(&crons)

	cronMessage := tgbotapi.NewMessage(chatId, "_Планировщик_")
	cronMessage.ParseMode = tgbotapi.ModeMarkdownV2
	var rows = make([][]tgbotapi.InlineKeyboardButton, 0)
	for _, c := range crons {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("%.2d:%.2d", c.Hour, c.Minute),
				cronAction(CronActionChangeHour, c),
			),
			tgbotapi.NewInlineKeyboardButtonData("❌", cronAction(CronActionDelete, c)),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("Добавить", cronAction(CronActionChangeHour, Cron{})),
	))
	cronMessage.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	_, _ = bot.ReSend(cronMessage)
}

func showStat() {
	var activeCount, removedCount, publishedCount = getStat()
	var config = tgbotapi.NewMessage(
		adminGroupId,
		fmt.Sprintf("*Не опубликованных*: %d\n*Удалённых*: %d\n*Опубликованных*: %d",
			activeCount, removedCount, publishedCount),
	)
	config.ParseMode = tgbotapi.ModeMarkdownV2
	result, err := bot.ReSend(config)
	fmt.Println(result, err)
}

func getStat() (int, int, int) {
	var activeCount, removedCount, publishedCount int
	_ = db.QueryRowx(`
		SELECT
			count(*) FILTER (WHERE status = 1) as active,
			count(*) FILTER (WHERE status = 2) as removed,
			count(*) FILTER (WHERE status = 3) as published
		FROM links
		WHERE links.chat_id = $1
	`, adminGroupId).Scan(&activeCount, &removedCount, &publishedCount)

	return activeCount, removedCount, publishedCount
}

func cronAction(action int, cron Cron) string {
	changeData, _ := json.Marshal(Action{
		Action: action,
		Cron:   cron,
	})
	return string(changeData)
}

func delAlbum(messageId int, originalMessageId int) {
	var medias []Media
	_ = db.Select(&medias, `
		SELECT _m2.link_id, _m2.message_id
		FROM media _m1
		JOIN links l on _m1.link_id = l.id AND l.chat_id = $1
		JOIN media _m2 ON _m2.link_id = _m1.link_id
		WHERE _m1.message_id = $2`,
		adminGroupId, messageId,
	)

	medias = append(medias, Media{
		MessageId: originalMessageId,
	})
	for _, media := range medias {
		deleteMessage := tgbotapi.NewDeleteMessage(adminGroupId, media.MessageId)
		_, _ = bot.Send(deleteMessage)
	}

	_, _ = db.Exec(`
		UPDATE links SET status = $1
		WHERE id = $2`,
		StatusDeleted, medias[0].LinkId,
	)
}

func isValidUrl(path string) bool {
	_, err := url.ParseRequestURI(path)
	if err != nil {
		return false
	}

	u, err := url.Parse(path)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}

	return true
}

func getAlbums(siteLink string) {
	doc, err := htmlquery.LoadURL(siteLink)
	if err != nil {
		return
	}

	albums := htmlquery.Find(doc, "//article[@id='content']/ul[contains(@class, 'gallery-a') "+
		"and not(contains(@class, 'clip-a'))]//li[not(contains(@class, 'vid'))]/a/@href")

	var activeCount, removedCount, publishedCount = getSavedCount(albums)
	var config = tgbotapi.NewMessage(
		adminGroupId,
		fmt.Sprintf("*Aльбомов*: %d\n*Уже в базе \\(Не опубликованных\\)*: %d\n*Удалённых*: %d\n*Опубликованных*: %d\n*Осталось распарсить*: %d", len(albums),
			activeCount, removedCount, publishedCount,
			len(albums)-activeCount-removedCount-publishedCount),
	)
	config.ParseMode = tgbotapi.ModeMarkdownV2
	if _, err = bot.ReSend(config); err != nil {
		time.Sleep(time.Second * time.Duration(10))
		return
	}

	for _, album := range albums {
		if parseStatus != 1 {
			bot.ReSend(tgbotapi.NewMessage(
				adminGroupId,
				"Процесс остановлен!",
			))
			return
		}
		getAlbum(album.FirstChild.Data)
	}
}

func getSavedCount(albums []*html.Node) (int, int, int) {
	var activeCount, removedCount, publishedCount int
	var links []string
	for _, album := range albums {
		links = append(links, album.FirstChild.Data)
	}
	_ = db.QueryRowx(`
		SELECT
			count(*) FILTER (WHERE status = 1) as active,
			count(*) FILTER (WHERE status = 2) as removed,
			count(*) FILTER (WHERE status = 3) as published
		FROM links
		WHERE links.chat_id = $1 AND links.link = any($2) 
	`, adminGroupId, pq.Array(links)).Scan(&activeCount, &removedCount, &publishedCount)

	return activeCount, removedCount, publishedCount
}

func getAlbum(albumUrl string) {
	link := Link{}
	if db.Get(&link, "SELECT id FROM links WHERE link=$1 and chat_id=$2 LIMIT 1", albumUrl, adminGroupId) == nil {
		return
	}

	doc, err := htmlquery.LoadURL(albumUrl)
	if err != nil {
		return
	}

	dir := filepath.Base(albumUrl)
	_ = os.Mkdir(dir, os.ModePerm)

	sizes := htmlquery.Find(doc, "//article[@id='content']/div[contains(@class, 'list-justified-container')]/ul/li/a/img//@srcset")
	count := len(sizes)
	if count < 10 {
		return
	}
	models := htmlquery.Find(doc, "//div[@class='link-btn']/h2[2]/a/text()")
	if models == nil {
		models = htmlquery.Find(doc, "//div[@class='link-btn']/h2[1]/a/text()")
	}
	model := models[0].Data

	// [0,2,4,6,7,8,9,11,13,14]
	keys := make([]int, 0, 10)
	keys = append(keys, 0)
	for i := 1; i <= 8; i++ {
		key := math.Round(float64(i) * float64(count-2) / 8.0)
		keys = append(keys, int(key))
	}
	keys = append(keys, count-1)

	// download files and set watermark
	var files []interface{}
	for key := range keys {
		var photoUrl = strings.Split(strings.Split(sizes[key].FirstChild.Data, ", ")[0], " ")[0]
		filename := downloadFile(dir, photoUrl)
		media := tgbotapi.NewInputMediaPhoto(dir + "/" + filename)
		if key == 0 {
			media.Caption = fmt.Sprintf("*Модель:* %s", model)
			media.ParseMode = tgbotapi.ModeMarkdownV2
		}
		files = append(files, media)
	}

	result, err := bot.ReSendMediaGroup(tgbotapi.NewMediaGroup(adminGroupId, files))
	if err != nil {
		time.Sleep(time.Minute * time.Duration(1))
		return
	}

	values := make([]string, 0, 10)
	for _, fileInfo := range result {
		values = append(values, fmt.Sprintf("('%s',%d)", getFileIdFromGroupMedia(fileInfo), fileInfo.MessageID))
	}

	if _, err = db.Exec(`
		WITH _data(file_id, message_id) AS (
			VALUES `+strings.Join(values, ", ")+`
		), _links AS (
			INSERT INTO links (link, status, model, chat_id)
			VALUES ($1, 1, $2, $3)
			ON CONFLICT DO NOTHING
			RETURNING id
		)
		INSERT INTO media (link_id, file_id, message_id)
		SELECT _links.id, _data.file_id, _data.message_id
		FROM _data
		CROSS JOIN _links`,
		albumUrl,
		model,
		adminGroupId); err != nil {
		panic(err)
	}
	_ = os.RemoveAll("./" + dir + "/")
	for i := 1; i <= 60; i++ {
		if parseStatus != 1 {
			return
		}
		time.Sleep(time.Second * time.Duration(1))
	}
}

func getFileIdFromGroupMedia(message tgbotapi.Message) string {
	return (message.Photo)[len(message.Photo)-1].FileID
}

func downloadFile(dir string, photoUrl string) string {
	filename := filepath.Base(photoUrl)
	resp, _ := http.Get(photoUrl)
	defer resp.Body.Close()

	setWatermark(dir, filename, resp.Body)
	return filename
}

func setWatermark(dir string, filename string, fileMain io.ReadCloser) {
	imageMain, err := jpeg.Decode(fileMain)
	if err != nil {
		log.Fatalf("failed to decode: %s", err)
	}

	var offset image.Point
	if os.Getenv("WatermarkPosition") == "BR" {
		offset = image.Pt(imageMain.Bounds().Max.X-70, imageMain.Bounds().Max.Y-70)
	} else {
		offset = image.Pt(20, imageMain.Bounds().Max.Y-70)
	}
	bounds := imageMain.Bounds()
	imageResult := image.NewRGBA(bounds)
	draw.Draw(imageResult, bounds, imageMain, image.Point{}, draw.Src)
	draw.Draw(imageResult, imageWatermark.Bounds().Add(offset), imageWatermark, image.Point{}, draw.Over)

	fileResult, err := os.Create(dir + "/" + filename)
	if err != nil {
		log.Fatalf("failed to create: %s", err)
	}
	defer fileResult.Close()
	_ = jpeg.Encode(fileResult, imageResult, &jpeg.Options{Quality: 100})
}
