// tiny is the tinyURL clone written on go, without the fancy features
// It receives the request as url param 'url' and shorten it to
// standardized format and stores the data for later use
package main

import (
	"fmt"
	"html/template"
	"math/rand"
	"net/http"
	"time"

	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/petrkotas/gotiny/pkg/utils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
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
	spanCtx, _ := a.tracer.Extract(opentracing.HTTPHeaders, opentracing.HTTPHeadersCarrier(r.Header))
	span := a.tracer.StartSpan("handleShorten", ext.RPCServerOption(spanCtx))
	defer span.Finish()

	span.LogFields(
		otlog.String("event", "handle-shorten"),
	)

	utils.RequestCounter.WithLabelValues("tiny", "handle_shorten", r.URL.RawQuery).Inc()
	query := r.URL.Query()
	urls, ok := query["url"]

	if !ok || len(urls[0]) < 1 {
		a.logger.WithFields(utils.StandardLogFields).Debug("Nothing to do")
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
			a.logger.WithFields(utils.StandardLogFields).WithError(err).Error("Cannot open result.html template file")
		}
	}

	t.Execute(w, p)
}

func (a *App) handleRedirect(w http.ResponseWriter, r *http.Request) {
	spanCtx, _ := a.tracer.Extract(opentracing.HTTPHeaders, opentracing.HTTPHeadersCarrier(r.Header))
	span := a.tracer.StartSpan("handleRedirect", ext.RPCServerOption(spanCtx))
	defer span.Finish()

	span.LogFields(
		otlog.String("event", "handle-redirect"),
	)

	utils.RequestCounter.WithLabelValues("tiny", "handle_redirect", r.URL.RawQuery).Inc()
	qs, ok := r.URL.Query()["q"]

	if !ok || len(qs[0]) < 1 {
		a.logger.WithFields(utils.StandardLogFields).Debug("Nothing to do.")
		return
	}

	fullURL, err := a.storage.Get(qs[0])
	if err != nil {
		a.logger.WithFields(utils.StandardLogFields).WithField("shorturl", qs[0]).Error("No short URL found")
	}

	a.logger.WithFields(utils.StandardLogFields).WithField("url", fullURL).Debug("Redirecting URL")
	http.Redirect(w, r, fullURL, http.StatusSeeOther)
}

func (a *App) handleMain(w http.ResponseWriter, r *http.Request) {
	spanCtx, _ := a.tracer.Extract(opentracing.HTTPHeaders, opentracing.HTTPHeadersCarrier(r.Header))
	span := a.tracer.StartSpan("handleMain", ext.RPCServerOption(spanCtx))
	defer span.Finish()

	span.LogFields(
		otlog.String("event", "render-main"),
	)

	utils.RequestCounter.WithLabelValues("tiny", "handle_main", r.URL.RawQuery).Inc()
	t, err := template.ParseFiles("tiny.html")
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte("Cannot load the template properly"))
		a.logger.WithFields(utils.StandardLogFields).WithError(err).Error("Cannot parse template for tiny.html")
	}

	t.Execute(w, nil)
}

func main() {
	tracer, closer := utils.InitTracer("gotiny-app")
	opentracing.SetGlobalTracer(tracer)
	defer closer.Close()

	app := App{
		storage:  make(urlStorage),
		serveURL: fmt.Sprintf(":%s", utils.GetEnv("TINY_PORT", "8888")),
		logger:   utils.NewLogger(),
		tracer:   tracer,
	}

	http.HandleFunc("/shorten", app.handleShorten)
	http.HandleFunc("/r", app.handleRedirect)
	http.HandleFunc("/", app.handleMain)
	http.Handle("/metrics", promhttp.Handler())
	app.logger.WithFields(utils.StandardLogFields).Fatal(http.ListenAndServe(app.serveURL, nil))
}

func init() {
	rand.Seed(time.Now().UnixNano())
	prometheus.MustRegister(utils.RequestCounter)
}
