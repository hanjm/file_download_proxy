package main

import (
	"bytes"
	"fmt"
	"github.com/hanjm/log"
	"html/template"
	"net/http"
	_ "net/http/pprof"
)

func HTTPServer(tm *TasksManager, serverAddr string, port int) {
	http.Handle("/download/", http.StripPrefix("/download", http.FileServer(http.Dir(tm.downloadDir))))
	http.HandleFunc("/file_download_proxy/ws", tm.WebSocketHandler)
	http.HandleFunc("/file_download_proxy/task", tm.TaskHandler)
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
	log.Infof("request api serverAddr:%s", serverAddr)
	http.Handle("/file_download_proxy/", index)
	listenAddr := fmt.Sprintf(":%d", port)
	log.Infof("service start at %s", listenAddr)
	log.Fatal(http.ListenAndServe(listenAddr, nil))
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

func (h *indexHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write(h.htmlData.Bytes())
}

func ReRenderIndexHtml() error {
	if index != nil {
		return index.reRender()
	}
	return nil
}
