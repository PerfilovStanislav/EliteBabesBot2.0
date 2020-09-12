package main

import (
	"github.com/antchfx/htmlquery"
	"github.com/disintegration/imaging"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
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
	"strings"
	"sync"
)

const (
	StateStart    = 0
	StateInSearch = 1
	WaterMark     = "555.png"
)

var (
	bot            *Bot
	db             *sqlx.DB
	imageWatermark image.Image
)

type Bot struct {
	*tgbotapi.BotAPI
}

func initBot() {
	bot = NewBot(os.Getenv("EliteBabesMultiParseBotToken"))
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
	//getAlbum(albums[2].FirstChild.Data)
	//for _, album := range albums {
	//	getAlbum(album.FirstChild.Data)
	//}
}

func getAlbum(albumUrl string) {
	doc, err := htmlquery.LoadURL(albumUrl)
	if err != nil {
		return
	}

	dir := filepath.Base(albumUrl)
	_ = os.Mkdir(dir, os.ModePerm)

	var wg sync.WaitGroup
	sizes := htmlquery.Find(doc, "//article[@id='content']/div[contains(@class, 'list-justified-container')]/ul/li/a/img//@srcset")
	count := len(sizes)
	if count == 0 {
		return
	}

	// [0,2,4,6,7,8,9,11,13,14]
	keys := make([]int, 0, 10)
	keys = append(keys, 0)
	for i := 1; i <= 8; i++ {
		key := math.Round(float64(i) * float64(count-2) / 8.0)
		keys = append(keys, int(key))
	}
	keys = append(keys, count-1)

	for key := range keys {
		var photoUrl = strings.Split(strings.Split(sizes[key].FirstChild.Data, ", ")[0], " ")[0]
		wg.Add(1)
		downloadFile(dir, photoUrl, &wg)
	}
	wg.Wait()
}

func downloadFile(dir string, photoUrl string, wg *sync.WaitGroup) {
	defer wg.Done()
	resp, _ := http.Get(photoUrl)
	defer resp.Body.Close()

	filename := filepath.Base(photoUrl)
	//out, err := os.Create(dir + "/" + filename)
	//if err != nil {
	//	panic(err)
	//}
	//defer out.Close()
	//io.Copy(out, resp.Body)

	setWatermark(dir, filename, resp.Body)
}

func setWatermark(dir string, filename string, body io.ReadCloser) {
	fileMain := body
	//if err != nil {
	//	log.Fatalf("failed to open: %s", err)
	//}
	//defer fileMain.Close()

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
