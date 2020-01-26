// tiny is the tinyURL clone written on go, without the fancy features
// It receives the request as url param 'url' and shorten it to
// standardized format and stores the data for later use
package main

import (
	"fmt"
	"html/template"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/opentracing/opentracing-go"
	"github.com/petrkotas/gotiny/pkg/utils"
)

// Storage represent url shortener storage
type Storage interface {
	Set(short, full string)
	Get(short string) (string, error)
}

type urlStorage map[string]string

func (s urlStorage) Set(short, full string) {
	s[short] = full
}

func (s urlStorage) Get(short string) (string, error) {
	full, ok := s[short]
	if !ok {
		return "", fmt.Errorf("No key found")
	}
	return full, nil
}

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_")

// Page contains the data for template rendering
type Page struct {
	// Url is the shortened URL
	URL string
}

// App is the main tiny app object
type App struct {
	storage  Storage
	serveURL string
	logger   log.Logger
	tracer   opentracing.Tracer
}

func randString(n int) string {
	b := make([]rune, n)

	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}

	return string(b)
}

func (a *App) handleShorten(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	urls, ok := query["url"]

	if !ok || len(urls[0]) < 1 {
		log.Print("Nothing to do")
		return
	}

	randStr := randString(32)
	a.storage.Set(randStr, urls[0])

	p := Page{URL: randStr}

	t, err := template.ParseFiles("result.html")
	if err != nil {
		if err != nil {
			w.WriteHeader(500)
			w.Write([]byte("Cannot load the template properly"))
			log.Print("Cannot open result.html template file")
		}
	}

	t.Execute(w, p)
}

func (a *App) handleRedirect(w http.ResponseWriter, r *http.Request) {
	qs, ok := r.URL.Query()["q"]

	if !ok || len(qs[0]) < 1 {
		log.Print("Nothing to do.")
		return
	}

	fullURL, err := a.storage.Get(qs[0])
	if err != nil {
		log.Print("No short URL found")
	}

	log.Print("Redirecting URL")
	http.Redirect(w, r, fullURL, http.StatusSeeOther)
}

func (a *App) handleMain(w http.ResponseWriter, r *http.Request) {
	t, err := template.ParseFiles("tiny.html")
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte("Cannot load the template properly"))
		log.Print("Cannot parse template for tiny.html")
	}

	t.Execute(w, nil)
}

func main() {
	app := App{
		storage:  make(urlStorage),
		serveURL: fmt.Sprintf(":%s", utils.GetEnv("TINY_PORT", "8888")),
	}

	http.HandleFunc("/shorten", app.handleShorten)
	http.HandleFunc("/r", app.handleRedirect)
	http.HandleFunc("/", app.handleMain)

	log.Fatal(http.ListenAndServe(app.serveURL, nil))
}

func init() {
	rand.Seed(time.Now().UnixNano())
}
