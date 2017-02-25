package main

//go:generate go-bindata ./static

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

//3GB limit
const LIMIT_SIZE = 3 * 1024 * 1024 * 1024

// relative dir
const DOWNLOAD_DIRNAME = "download"

var bind_addr string
var index_data bytes.Buffer
var files_info = map[string]*FileInfo{}

func getAsset(path string) []byte {
	data, err := Asset(path)
	if err != nil {
		log.Printf("Asset not found, try 'go-bindata ./static' again")
		return []byte{}
	}
	return data
}

func init() {
	//parse addr:port args
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: download_proxy addr:port\n\n")
	}
	flag.Parse()

	if len(os.Args) > 1 {
		bind_addr = os.Args[1]
	} else {
		fmt.Fprintf(os.Stderr, "Usage: download_proxy addr:port\n\n")
		os.Exit(1)
	}

	//cache index template
	index_template := template.New("index.html")
	index_template.Parse(string(getAsset("static/index.html")))
	//index_template, _ := template.ParseFiles("index.html")

	type Context struct {
		Bind_addr string
	}
	context := Context{Bind_addr: bind_addr}
	index_template.Execute(&index_data, context)
}

//list files handler
func files_info_handler(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	switch req.Method {
	case "GET":
		var response []byte
		ListFiles(DOWNLOAD_DIRNAME)
		response, _ = json.Marshal(files_info)
		w.Header().Set("Content-Type", "application/json")
		w.Write(response)
	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

//file_operation_handler handle file download(get) / create download task(post) / delete_file(delete)
func file_operation_handler(w http.ResponseWriter, req *http.Request) {
	filename := req.URL.Query().Get("filename")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	switch req.Method {
	case "GET":
		if filename == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		log.Printf("Download %v", filename)
		http.Redirect(w, req, "/download/"+filename, http.StatusTemporaryRedirect)
	case "POST":
		download_url := req.PostFormValue("url")
		if download_url == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var response []byte
		// check total size
		files_size := ListFiles(DOWNLOAD_DIRNAME)
		if files_size > LIMIT_SIZE {
			w.WriteHeader(http.StatusServiceUnavailable)
			response, _ = json.Marshal(map[string]interface{}{"Message": "There are too many files in server, please delete some files", "FilesSize": get_human_size_string(files_size)})
		} else {
			new_file_info := new(FileInfo)
			new_file_info.SourceUrl = download_url
			new_file_info.FileName = get_safe_filename(download_url)
			files_info[new_file_info.FileName] = new_file_info
			go fetch_file(new_file_info)
			w.WriteHeader(http.StatusCreated)
			response, _ = json.Marshal(map[string]string{"Message": "CREATE OK"})
		}
		w.Header().Set("Content-Type", "json")
		w.Write(response)
	case "DELETE":
		log.Printf("Delete %v", filename)
		var response []byte
		if filename == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		err := delete_file(filename)
		if err != nil {
			log.Printf("Delete Error when delete %v:%v", filename, err)
			w.WriteHeader(http.StatusNotFound)
			response, _ = json.Marshal(map[string]string{"Message": err.Error()})
		} else {
			response, _ = json.Marshal(map[string]string{"Message": "DELETE OK"})
		}

		w.Header().Set("Content-Type", "json")
		w.Write(response)
	case "OPTIONS":
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, DELETE")

	default:
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(req.Method))
	}
}

func main() {
	//make dir and init
	os.Mkdir("download", 0777)
	files, _ := ioutil.ReadDir(DOWNLOAD_DIRNAME)
	for _, file := range files {
		if file.IsDir() {
			continue
		} else {
			file_size := file.Size()
			filename := file.Name()
			new_file_info := FileInfo{
				FileName:       filename,
				SourceUrl:      "Local",
				Size:           file_size,
				ContentLength:  file_size,
				StartTimeStamp: file.ModTime().Unix(),
				Duration:       0,
				Speed:          0,
				IsDownloaded:   true,
				IsError:        false}
			files_info[filename] = &new_file_info
		}
	}

	// running aria2c with enable rpc once
	if has_aria2c() && !is_aria2c_running {
		go func() {
			is_aria2c_running = true
			cmd := exec.Command("aria2c", "--dir=download", "--enable-rpc", "--rpc-listen-port=6900", "--rpc-listen-all=false")
			err := cmd.Start()
			if err != nil {
				log.Println("aria2c can not start :", err.Error())
				is_aria2c_running = false
			}
			time.Sleep(0)
			cmd.Wait()
		}()
	} else {
		log.Println("aria2c not install,cannot download magnet")
	}

	//http server
	//http.Handle("/static/", http.StripPrefix("/static", http.FileServer(http.Dir("static/"))))
	http_redirect("/", "/proxy")
	http.Handle("/download/", http.StripPrefix("/download", http.FileServer(http.Dir("download"))))
	http.HandleFunc("/proxy/files", files_info_handler)
	http.HandleFunc("/proxy/file", file_operation_handler)
	http.HandleFunc("/favicon.ico", func(w http.ResponseWriter, req *http.Request) {
		//http.ServeFile(w, req, "favicon.ico")
		w.Write(getAsset("static/favicon.ico"))
	})
	http.HandleFunc("/proxy/", func(w http.ResponseWriter, req *http.Request) {
		w.Write(index_data.Bytes())
	})
	fmt.Printf("service start at http://127.0.0.1:%v\n", strings.Split(bind_addr, ":")[1])
	log.Fatal(http.ListenAndServe(bind_addr, nil))
}
