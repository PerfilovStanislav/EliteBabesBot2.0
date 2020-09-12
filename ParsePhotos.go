package main

import (
	"fmt"
	"github.com/antchfx/htmlquery"
	"github.com/disintegration/imaging"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"golang.org/x/image/draw"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"telegram-bot-api"
)

const (
	StateStart    = 0
	StateInSearch = 1
	WaterMark     = "watermark.png"
)

var (
	bot            *Bot
	db             *sqlx.DB
	imageWatermark image.Image
	adminChannelId int64
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
	adminChannelId, _ = strconv.ParseInt(os.Getenv("AdminChannelId"), 10, 64)

	//updates := bot.ListenForWebhook("/" + bot.Token)
	//go http.ListenAndServe(":3001", nil)
	//for update := range updates {
	//	if update.Message != nil {
	//		if update.Message.Text == "/start" {
	//			_, _ = bot.sendSearchKeyboard(update.Message.Chat.ID)
	//		} else if update.Message.Text == "Search" {
	//			anonymSearchHandler(AnonymSearch{
	//				UserId:update.Message.Chat.ID,
	//				LanguageCode: update.Message.From.LanguageCode,
	//			})
	//		}
	//	}
	//}

	getAlbums()
}

func getAlbums() {
	var siteLink = "https://www.elitebabes.com/"

	doc, err := htmlquery.LoadURL(siteLink)
	if err != nil {
		return
	}

	albums := htmlquery.Find(doc, "//article[@id='content']/ul[contains(@class, 'gallery-a') "+
		"and not(contains(@class, 'clip-a'))]//li[not(contains(@class, 'vid'))]/a/@href")

	getAlbum(albums[0].FirstChild.Data)
	//getAlbum(albums[1].FirstChild.Data)
	//getAlbum(albums[1].FirstChild.Data)
	//getAlbum(albums[2].FirstChild.Data)
	//for _, album := range albums {
	//	getAlbum(album.FirstChild.Data)
	//}
}

func getAlbum(albumUrl string) {
	link := Link{}
	if db.Get(&link, "SELECT id FROM links WHERE link=$1 LIMIT 1", albumUrl) == nil {
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
	for i := 1; i <= 3; i++ {
		key := math.Round(float64(i) * float64(count-2) / 3.0)
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
		files = append(files, tgbotapi.NewInputMediaPhoto(dir+"/"+filename))
	}
	wg.Wait()

	result, _ := bot.SendMediaGroup(tgbotapi.NewMediaGroup(adminChannelId, files))

	values := make([]string, 0, 5)
	for _, fileInfo := range result {
		values = append(values, fmt.Sprintf("('%s',%d)", getFileIDFromMsg(fileInfo), fileInfo.MessageID))
	}

	if _, err = db.Exec(`
		WITH _data(file_id, message_id) AS (
			VALUES `+strings.Join(values, ", ")+`
		), _links AS (
			INSERT INTO links (link, status, model)
			VALUES ($1, 1, $2)
			ON CONFLICT DO NOTHING
			RETURNING id
		)
		INSERT INTO media (link_id, file_id, message_id)
		SELECT _links.id, _data.file_id, _data.message_id
		FROM _data
		CROSS JOIN _links`,
		albumUrl,
		model); err != nil {
		panic(err)
	}
}

func getFileIDFromMsg(message tgbotapi.Message) string {
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
