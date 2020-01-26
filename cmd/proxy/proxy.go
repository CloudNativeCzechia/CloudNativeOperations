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

	"github.com/petrkotas/gotiny/pkg/utils"
)

type urlCache map[string][]byte
type contextKey string

// App is the main proxy object
type App struct {
	cache    urlCache
	proxyURL string
	serveURL string
}

// serveReverseProxy for a giver URL
// for gotiny usecase it is only single URL counting the requests
func (a *App) serveReverseProxy(ctx context.Context, target string, res http.ResponseWriter, req *http.Request) {

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

	// Note that ServeHttp is non blocking and uses a go routine under the hood
	proxy.ServeHTTP(res, req)
}

// cacheResponse stores the response in cache for further use
func (a *App) cacheResponse(ctx context.Context) func(response *http.Response) error {
	// use decorator to put context into the response processing function
	return func(response *http.Response) error {
		dump, err := httputil.DumpResponse(response, true)
		if err != nil {
			return err
		}

		ctx := response.Request.Context()
		requestInterface := ctx.Value(contextKey("APIRequest"))
		if requestInterface == nil {
			// not an error, just no context
			return nil
		}

		requestURL := requestInterface.(string)

		hash := sha1.New()
		n, err := io.WriteString(hash, requestURL)
		if err != nil {
			return err
		}
		if n != len(requestURL) {
			return errors.New("Written length is not the same as original")
		}
		key := base64.StdEncoding.EncodeToString(hash.Sum(nil))

		a.cache[key] = dump

		return nil
	}

}

// parseRequest returns URL to be shortened
func (a *App) parseRequest(ctx context.Context, request *http.Request) string {

	keys, ok := request.URL.Query()["url"]

	if !ok || len(keys[0]) < 1 {
		return ""
	}

	// Query()["url"] will return an array of items,
	// we only want the single item.
	return keys[0]
}

func (a *App) getCache(ctx context.Context, req *http.Request) *http.Response {
	hash := sha1.New()
	n, err := io.WriteString(hash, req.URL.RequestURI())
	if err != nil {
		return nil
	}
	if n != len(req.URL.RequestURI()) {
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
		return nil
	}

	return response
}

// Given a request send it to the appropriate url
// This is the entry point for user requests and origin for context
func (a *App) handleRequestAndRedirect(res http.ResponseWriter, req *http.Request) {

	ctx := req.Context()

	response := a.getCache(ctx, req)
	if response != nil {

		for k, v := range response.Header {
			val := ""
			if len(v) != 0 {
				val = v[0]
			}
			res.Header().Set(k, val)
		}
		io.Copy(res, response.Body)
		response.Body.Close()

		return
	}

	// ctx = context.WithValue(ctx, contextKey("APIRequest"), req.URL.RequestURI())
	// req = req.WithContext(ctx)

	a.serveReverseProxy(ctx, a.proxyURL, res, req)
}

// Main entry point for the proxy
func main() {
	app := App{
		cache:    make(urlCache),
		serveURL: fmt.Sprintf(":%s", utils.GetEnv("PROXY_PORT", "8888")),
		proxyURL: utils.GetEnv("TINY_URL", ""),
	}

	// start server
	http.HandleFunc("/", app.handleRequestAndRedirect)

	if err := http.ListenAndServe(app.serveURL, nil); err != nil {
		panic(err)
	}
}

func init() {
}
