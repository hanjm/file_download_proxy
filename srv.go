package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"github.com/hanjm/log"
	"html/template"
	"net/http"
	_ "net/http/pprof"
)

func HTTPServer(tm *TasksManager, serverAddr string, port int, basicAuth string) {
	http.Handle("/download/", http.StripPrefix("/download", http.FileServer(http.Dir(tm.downloadDir))))
	http.Handle("/file_download_proxy/ws", http.HandlerFunc(tm.WebSocketHandler))
	http.Handle("/file_download_proxy/task", http.HandlerFunc(tm.TaskHandler))
	http.HandleFunc("/favicon.ico", func(w http.ResponseWriter, req *http.Request) {
		http.ServeFile(w, req, "favicon.ico")
	})
	index = &indexHandler{
		templateContext: map[string]string{
			"ServerAddr": serverAddr,
		},
		htmlData: new(bytes.Buffer),
	}
	err := index.reRender()
	if err != nil {
		log.Errorf("render index.html error:%s", err)
	}
	http.Handle("/file_download_proxy/", index)
	log.Infof("request api serverAddr:%s", serverAddr)
	listenAddr := fmt.Sprintf(":%d", port)
	log.Infof("service start at %s", listenAddr)
	log.Fatal(http.ListenAndServe(listenAddr, Auth(http.DefaultServeMux, basicAuth)))
}

var index *indexHandler

type indexHandler struct {
	templateContext map[string]string
	htmlData        *bytes.Buffer
}

func (h *indexHandler) reRender() (err error) {
	//cache index template
	indexTemplate, err := template.ParseFiles("index.html")
	if err != nil {
		return fmt.Errorf("parse index.html error:%s", err)
	}
	htmlData := new(bytes.Buffer)
	indexTemplate.Execute(htmlData, h.templateContext)
	h.htmlData = htmlData
	return nil
}

func (h *indexHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write(h.htmlData.Bytes())
}

func ReRenderIndexHtml() error {
	if index != nil {
		return index.reRender()
	}
	return nil
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
