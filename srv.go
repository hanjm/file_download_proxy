package main

import (
	"encoding/base64"
	"fmt"
	"github.com/hanjm/log"
	"net/http"
	_ "net/http/pprof"
)

func HTTPServer(tm *TasksManager, port int, basicAuth string) {
	http.Handle("/download/", http.StripPrefix("/download", http.FileServer(http.Dir(tm.downloadDir))))
	http.Handle("/file_download_proxy/ws", http.HandlerFunc(tm.WebSocketHandler))
	http.Handle("/file_download_proxy/task", http.HandlerFunc(tm.TaskHandler))
	http.HandleFunc("/favicon.ico", HandleFile("favicon.ico"))
	http.Handle("/file_download_proxy/", HandleFile("index.html"))
	listenAddr := fmt.Sprintf(":%d", port)
	log.Infof("service start at %s", listenAddr)
	log.Fatal(http.ListenAndServe(listenAddr, Auth(http.DefaultServeMux, basicAuth)))
}

func HandleFile(filename string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		http.ServeFile(w, req, filename)
	}
}

type BasicAuthHandler struct {
	Token string
	Next  http.Handler
}

func (b *BasicAuthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if b.Token != "" && r.Header.Get("Authorization") != b.Token {
		w.Header().Set("WWW-Authenticate", `Basic realm="StatusUnauthorized"`)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	b.Next.ServeHTTP(w, r)
}

func Auth(handler http.Handler, basicAuth string) http.Handler {
	var token string
	if basicAuth != "" {
		token = fmt.Sprintf("Basic %s", base64.StdEncoding.EncodeToString([]byte(basicAuth)))
	}
	return &BasicAuthHandler{token, handler}
}
