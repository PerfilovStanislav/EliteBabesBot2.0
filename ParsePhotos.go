package main

import (
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
	"sync"
	"telegram-bot-api"
	"time"
)

const (
	WaterMark     = "watermark.png"
	StatusActive  = 1
	StatusDeleted = 2
)

var (
	bot            *Bot
	db             *sqlx.DB
	imageWatermark image.Image
	adminGroupId   int64
)

type Bot struct {
	*tgbotapi.BotAPI
}

func initBot() {
	bot = NewBot(os.Getenv("EliteBabesMultiParseBotToken"))
	bot.Debug = true
	//_, _ = bot.SetWebhook(tgbotapi.NewWebhook("https://aba3f4f4e933.ngrok.io/" + bot.Token))
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

func main() {
	initDb(os.Getenv("DB_NAME"))
	initBot()
	initWatermark()
	adminGroupId, _ = strconv.ParseInt(os.Getenv("AdminGroupId"), 10, 64)

	webHook := tgbotapi.NewWebhook("https://89a8bed9aa2d.ngrok.io/go/" + bot.Token)
	result, err := bot.Send(webHook)
	fmt.Println(result, err)

	updates := bot.ListenForWebhook("/go/" + bot.Token)
	go http.ListenAndServe(":3005", nil)

	for update := range updates {
		if isValidUrl(update.Message.Text) {
			getAlbums(update.Message.Text)
		}
		if update.Message.ReplyToMessage != nil {
			text := update.Message.Text
			if text == "/del" || text == "/del@EliteBabesMultiParseBot" {
				var medias []Media
				_ = db.Select(&medias, `
					select _m2.link_id, _m2.message_id
					from media _m1
					join media _m2 ON _m2.link_id = _m1.link_id
					where _m1.message_id = $1
				`, update.Message.ReplyToMessage.MessageID)

				medias = append(medias, Media{
					MessageId: update.Message.ReplyToMessage.MessageID,
				})
				for _, media := range medias {
					deleteMessage := tgbotapi.NewDeleteMessage(adminGroupId, media.MessageId)
					if _, err := bot.Send(deleteMessage); err != nil {
						panic(err)
					}
				}

				db.Exec(`
					UPDATE links SET status = $1
					WHERE id = $2`,
					StatusDeleted, medias[0].LinkId)

			}
		}
	}
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

	var activeCount, removedCount = getSavedCount(albums)
	var config = tgbotapi.NewMessage(
		adminGroupId,
		fmt.Sprintf("*Aльбомов*: %d\n*Активных*: %d\n*Удалённых*: %d", len(albums), activeCount, removedCount),
	)
	config.ParseMode = tgbotapi.ModeMarkdownV2
	_, _ = bot.Send(config)

	for _, album := range albums {
		getAlbum(album.FirstChild.Data)
	}
}

func getSavedCount(albums []*html.Node) (int, int) {
	var activeCount, removedCount int
	var links []string
	for _, album := range albums {
		links = append(links, album.FirstChild.Data)
	}
	_ = db.QueryRowx(`
		SELECT count(*) FILTER (WHERE status = 1) as active, count(*) FILTER (WHERE status = 2) as removed
		FROM links
		WHERE links.chat_id = $1 AND links.link = any($2) 
	`, adminGroupId, pq.Array(links)).Scan(&activeCount, removedCount)

	return activeCount, removedCount
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
	model := htmlquery.Find(doc, "//div[@class='link-btn']/h2[2]/a/text()")[0].Data

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
	var wg sync.WaitGroup
	for key := range keys {
		var photoUrl = strings.Split(strings.Split(sizes[key].FirstChild.Data, ", ")[0], " ")[0]
		wg.Add(1)
		filename := downloadFile(dir, photoUrl, &wg)
		media := tgbotapi.NewInputMediaPhoto(dir + "/" + filename)
		if key == 0 {
			media.Caption = fmt.Sprintf("*Модель:* %s", model)
			media.ParseMode = tgbotapi.ModeMarkdownV2
		}
		files = append(files, media)
	}
	wg.Wait()

	result, err := bot.SendMediaGroup(tgbotapi.NewMediaGroup(adminGroupId, files))
	if err != nil {
		panic(err)
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
	time.Sleep(time.Minute * time.Duration(1))
}

func getFileIdFromGroupMedia(message tgbotapi.Message) string {
	return (message.Photo)[len(message.Photo)-1].FileID
}

func downloadFile(dir string, photoUrl string, wg *sync.WaitGroup) string {
	defer wg.Done()
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

	offset := image.Pt(20, imageMain.Bounds().Max.Y-70)
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

func NewBot(token string) *Bot {
	bot, _ := tgbotapi.NewBotAPI(token)
	return &Bot{
		BotAPI: bot,
	}
}
