// Proxy for gotiny to cound te requests and cache responses
// the configuration is acquired from environment variables:
// PORT: for port on which to serve the proxy (defaults: 1338)
// TINYURL: for url where gotiny is running
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"

	opentracing "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/petrkotas/gotiny/pkg/utils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
)

type urlCache map[string][]byte
type contextKey string

// App is the main proxy object
type App struct {
	cache    urlCache
	logger   log.Logger
	tracer   opentracing.Tracer
	proxyURL string
	serveURL string
}

// serveReverseProxy for a giver URL
// for gotiny usecase it is only single URL counting the requests
func (a *App) serveReverseProxy(ctx context.Context, target string, res http.ResponseWriter, req *http.Request) {
	utils.RequestCounter.WithLabelValues("proxy", "handle_external_request_proxy", req.URL.RawQuery).Inc()

	span, _ := opentracing.StartSpanFromContext(ctx, "serveReverseProxy")
	defer span.Finish()

	span.LogFields(
		otlog.String("event", "serveReverseProxy"),
	)

	// parse the url
	url, _ := url.Parse(target)

	// create the reverse proxy
	proxy := httputil.NewSingleHostReverseProxy(url)

	// Update the headers to allow for SSL redirection
	req.URL.Host = url.Host
	req.URL.Scheme = url.Scheme
	req.Header.Set("X-Forwarded-Host", req.Header.Get("Host"))
	req.Host = url.Host

	proxy.ModifyResponse = a.cacheResponse(ctx)

	// Inject tracing info into the request
	ext.SpanKindRPCClient.Set(span)
	ext.HTTPUrl.Set(span, target)
	ext.HTTPMethod.Set(span, req.Method)
	span.Tracer().Inject(
		span.Context(),
		opentracing.HTTPHeaders,
		opentracing.HTTPHeadersCarrier(req.Header),
	)

	// Note that ServeHttp is non blocking and uses a go routine under the hood
	proxy.ServeHTTP(res, req)
}

// cacheResponse stores the response in cache for further use
func (a *App) cacheResponse(ctx context.Context) func(response *http.Response) error {
	// use decorator to put context into the response processing function
	return func(response *http.Response) error {
		span, _ := opentracing.StartSpanFromContext(ctx, "cacheResponse")
		defer span.Finish()

		span.LogFields(
			otlog.String("event", "cacheResponse"),
		)

		dump, err := httputil.DumpResponse(response, true)
		if err != nil {
			a.logger.WithFields(utils.StandardLogFields).WithError(err).Error("Cannot dump response")
			return err
		}

		ctx := response.Request.Context()
		requestInterface := ctx.Value(contextKey("APIRequest"))
		if requestInterface == nil {
			// not an error, just no context
			a.logger.WithFields(utils.StandardLogFields).Warning("No context received")
			return nil
		}

		requestURL := requestInterface.(string)

		hash := sha1.New()
		n, err := io.WriteString(hash, requestURL)
		if err != nil {
			a.logger.WithFields(utils.StandardLogFields).WithError(err).Error("Cannot compute SHA1 hash for request in response")
			return err
		}
		if n != len(requestURL) {
			a.logger.WithFields(utils.StandardLogFields).WithError(err).Error("Cannot compute SHA1 hash for request in response")
			return errors.New("Written length is not the same as original")
		}
		key := base64.StdEncoding.EncodeToString(hash.Sum(nil))

		a.logger.WithFields(utils.StandardLogFields).WithField("key", key).Info("Cached response")

		a.cache[key] = dump

		return nil
	}

}

// parseRequest returns URL to be shortened
func (a *App) parseRequest(ctx context.Context, request *http.Request) string {
	span, _ := opentracing.StartSpanFromContext(ctx, "parseRequest")
	defer span.Finish()

	keys, ok := request.URL.Query()["url"]

	if !ok || len(keys[0]) < 1 {
		a.logger.WithFields(utils.StandardLogFields).WithField("URL", request.URL.String()).Info("No URL key")
		return ""
	}

	span.LogFields(
		otlog.String("event", "parseRequest"),
	)

	// Query()["url"] will return an array of items,
	// we only want the single item.
	return keys[0]
}

func (a *App) getCache(ctx context.Context, req *http.Request) *http.Response {
	span, _ := opentracing.StartSpanFromContext(ctx, "getCache")
	defer span.Finish()

	span.LogFields(
		otlog.String("event", "getCache"),
	)

	hash := sha1.New()
	n, err := io.WriteString(hash, req.URL.RequestURI())
	if err != nil {
		a.logger.WithFields(utils.StandardLogFields).WithError(err).Error("Cannot compute SHA1 hash for request")
		return nil
	}
	if n != len(req.URL.RequestURI()) {
		a.logger.WithFields(utils.StandardLogFields).WithError(err).Error("Cannot compute SHA1 hash for request")
		return nil
	}
	key := base64.StdEncoding.EncodeToString(hash.Sum(nil))

	cache, ok := a.cache[key]
	if !ok {
		return nil
	}

	r := bufio.NewReader(bytes.NewReader(cache))

	response, err := http.ReadResponse(r, req)
	if err != nil {
		a.logger.WithFields(utils.StandardLogFields).WithError(err).Error("Cannot read cached response")
		return nil
	}

	return response
}

// Given a request send it to the appropriate url
// This is the entry point for user requests and origin for context
func (a *App) handleRequestAndRedirect(res http.ResponseWriter, req *http.Request) {
	// Add one request to the proxy
	utils.RequestCounter.WithLabelValues("proxy", "handle_external_request", req.URL.RawQuery).Inc()

	// for every user request, create new main span
	span := a.tracer.StartSpan("handleRequestAndRedirect", opentracing.Tag{"requestURL", req.URL.RequestURI()})
	defer span.Finish()

	ctx := opentracing.ContextWithSpan(req.Context(), span)

	request := a.parseRequest(ctx, req)
	a.logger.WithFields(utils.StandardLogFields).WithFields(log.Fields{"requestURL": req.URL.RequestURI(), "proxyURL": a.proxyURL}).Info("Request received")

	span.LogFields(
		otlog.String("event", "handleRequestAndRedirect"),
	)

	response := a.getCache(ctx, req)
	if response != nil {
		utils.RequestCounter.WithLabelValues("proxy", "handle_external_request_cached", req.URL.RawQuery).Inc()

		for k, v := range response.Header {
			val := ""
			if len(v) != 0 {
				val = v[0]
			}
			res.Header().Set(k, val)
		}
		io.Copy(res, response.Body)
		response.Body.Close()
		a.logger.WithFields(utils.StandardLogFields).Info("Served from cache")

		return
	}

	ctx = context.WithValue(ctx, contextKey("APIRequest"), req.URL.RequestURI())
	req = req.WithContext(ctx)

	a.logger.WithFields(utils.StandardLogFields).WithField("requestURL", request).Info("Serving proxy")
	a.serveReverseProxy(ctx, a.proxyURL, res, req)
}

// Main entry point for the proxy
func main() {
	tracer, closer := utils.InitTracer("gotiny-proxy")
	opentracing.SetGlobalTracer(tracer)
	defer closer.Close()

	app := App{
		cache:    make(urlCache),
		logger:   utils.NewLogger(),
		tracer:   tracer,
		serveURL: fmt.Sprintf(":%s", utils.GetEnv("PROXY_PORT", "8888")),
		proxyURL: utils.GetEnv("TINY_URL", ""),
	}

	app.logger.WithFields(utils.StandardLogFields).Info("Started proxy")

	// start server
	http.HandleFunc("/", app.handleRequestAndRedirect)
	http.Handle("/metrics", promhttp.Handler())

	if err := http.ListenAndServe(app.serveURL, nil); err != nil {
		panic(err)
	}
}

func init() {
	prometheus.MustRegister(utils.RequestCounter)
}
