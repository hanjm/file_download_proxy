package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	//3GB limit
	LIMIT_SIZE = 3 * 1024 * 1024 * 1024
	// relative dir
	DOWNLOAD_DIRNAME = "download"
	//aria2c 配置
	ARIA2_ADD_URL_METHOD         = "aria2.addUri"
	ARIA2_TELL_STATUS_METHOD     = "aria2.tellStatus"
	ARIA2_REMOVE_DOWNLOAD_RESULT = "aria2.removeDownloadResult"
)

var (
	bindAddr               string
	indexData              bytes.Buffer
	isAria2cRunning        = false
	safeFilenameRegexp     = regexp.MustCompile(`[\w\d\-.]+`)
	testfileFilenameRegexp = regexp.MustCompile(`(test)?\d+[MmGg][Bb]?-.*`)
	fileTasks              = make(chan *FileInfo, 20)
	pushFilesUpdate        = make(chan struct{})
	connections            = &struct {
		mutex *sync.Mutex
		conns map[*websocket.Conn]struct{}
	}{
		mutex: new(sync.Mutex),
		conns: make(map[*websocket.Conn]struct{}),
	}
	fileInfos = &struct {
		mutex *sync.RWMutex
		infos map[string]*FileInfo
	}{
		mutex: new(sync.RWMutex),
		infos: make(map[string]*FileInfo),
	}
)

type FileInfo struct {
	FileName       string
	SourceUrl      string
	Size           int64
	ContentLength  int64
	StartTimeStamp int64
	Duration       int64
	Speed          int64
	IsDownloaded   bool
	IsError        bool
}

//aria2c rpc
type Aria2JsonRPCReq struct {
	Method  string        `json:"method"`
	Jsonrpc string        `json:"jsonrpc"`
	Id      string        `json:"id"`
	Params  []interface{} `json:"params"`
}
type Aria2JsonRPCError struct {
	Code    int64  `json:"code"`
	Message string `json:"message"`
}
type Aria2JsonRPCResp struct {
	Id      string      `json:"id"`
	Jsonrpc string      `json:"jsonrpc"`
	Result  interface{} `json:"result"`
	Error   Aria2JsonRPCError
}

func init() {
	//parse addr:port args
	if len(os.Args) > 1 {
		bindAddr = os.Args[1]
	} else {
		panic("\nUsage: go run main.go addr:port\nExample:go run main.go 127.0.0.1:8000")
	}
	//cache index template
	indexTemplate, _ := template.ParseFiles("index.html")
	type Context struct {
		BindAddr string
	}
	context := Context{BindAddr: bindAddr}
	indexTemplate.Execute(&indexData, context)
	// 10 Goroutines
	for i := 0; i < 10; i++ {
		go fetchFileWorker()
	}

}

//list files handler
func filesInfoHandler(w http.ResponseWriter, req *http.Request) {
	var upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	conn, err := upgrader.Upgrade(w, req, nil)
	if err != nil {
		log.Println("err when upgrader.Upgrade() ", err)
		return
	}
	connections.mutex.Lock()
	connections.conns[conn] = struct{}{}
	connections.mutex.Unlock()
	log.Println("new connection ", conn.RemoteAddr())
	// 首次连接,推送文件信息
	pushFilesUpdate <- struct{}{}
}

//fileOperationHandler handle file download(get) / create download task(post) / deleteFile(delete)
func fileOperationHandler(w http.ResponseWriter, req *http.Request) {
	filename := req.URL.Query().Get("filename")
	switch req.Method {
	case "GET":
		if filename == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		log.Printf("Download %v", filename)
		http.Redirect(w, req, "/download/"+filename, http.StatusTemporaryRedirect)
	case "POST":
		downloadUrl := strings.TrimSpace(req.PostFormValue("url"))
		if downloadUrl == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var response []byte
		// check total size
		filesSize := listFiles(DOWNLOAD_DIRNAME)
		if filesSize > LIMIT_SIZE {
			w.WriteHeader(http.StatusServiceUnavailable)
			response, _ = json.Marshal(map[string]interface{}{"Message": "There are too many files in server, please delete some files", "FilesSize": getHumanSizeString(filesSize)})
		} else {
			newFileInfo := new(FileInfo)
			newFileInfo.SourceUrl = downloadUrl
			newFileInfo.FileName = getSafeFilename(downloadUrl)
			fileInfos.mutex.Lock()
			fileInfos.infos[newFileInfo.FileName] = newFileInfo
			fileInfos.mutex.Unlock()
			fileTasks <- newFileInfo
			// 添加任务后,推送文件信息
			pushFilesUpdate <- struct{}{}
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
		err := deleteFile(filename)
		if err != nil {
			log.Printf("Delete Error when delete %v:%v", filename, err)
			w.WriteHeader(http.StatusNotFound)
			response, _ = json.Marshal(map[string]string{"Message": err.Error()})
		} else {
			response, _ = json.Marshal(map[string]string{"Message": "DELETE OK"})
		}

		w.Header().Set("Content-Type", "json")
		w.Write(response)

	default:
		w.WriteHeader(http.StatusBadRequest)

	}
}

func listFiles(dirname string) int64 {
	var fileSize int64
	files, _ := ioutil.ReadDir(dirname)
	for _, file := range files {
		var fileInfo *FileInfo
		fileInfos.mutex.RLock()
		fileInfo = fileInfos.infos[file.Name()]
		fileInfos.mutex.RUnlock()
		if fileInfo == nil {
			//rebuild new local file
			fileSize := file.Size()
			filename := file.Name()
			newFileInfo := FileInfo{
				FileName:       filename,
				SourceUrl:      "Local",
				Size:           fileSize,
				ContentLength:  fileSize,
				StartTimeStamp: file.ModTime().Unix(),
				Duration:       0,
				Speed:          0,
				IsDownloaded:   true,
				IsError:        false}
			fileInfos.mutex.Lock()
			fileInfos.infos[filename] = &newFileInfo
			fileInfos.mutex.Unlock()
			fileInfo = &newFileInfo
		}
		if fileInfo.Size != file.Size() {
			fileInfo.Size = file.Size()
		}
		if (!fileInfo.IsDownloaded) && (!fileInfo.IsError) {
			duration := time.Now().Unix() - fileInfo.StartTimeStamp
			if duration <= 0 {
				duration = 1
			}
			fileInfo.Speed = fileInfo.Size / duration
		} else {
			fileInfo.ContentLength = fileInfo.Size
		}
		fileSize += int64(math.Max(float64(file.Size()), float64(fileInfo.ContentLength)))
	}
	return fileSize
}

func deleteFile(filename string) error {
	fileInfos.mutex.RLock()
	fileInfo := fileInfos.infos[filename]
	fileInfos.mutex.RUnlock()
	if fileInfo != nil {
		if !fileInfo.IsDownloaded && !fileInfo.IsError {
			return errors.New("file is downloading..")
		}
		err := os.RemoveAll(DOWNLOAD_DIRNAME + "/" + filename)
		if err != nil {
			return err
		}
		fileInfos.mutex.Lock()
		delete(fileInfos.infos, filename)
		fileInfos.mutex.Unlock()
		return nil
	}
	log.Println(filename, fileInfos.infos)
	return errors.New("no such file or direcotry..")

}
func getContentLengthAndAttachmentFilename(httpClient *http.Client, url string) (int64, string, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return 0, "", err
	}
	contentLength := resp.ContentLength
	contentDisposition := strings.SplitN(resp.Header.Get("Content-Disposition"), "=", 2)
	resp.Body.Close()
	var attachmentName string
	if len(contentDisposition) > 1 {
		attachmentName = contentDisposition[1]
	}
	return contentLength, attachmentName, nil
}

func handleFetchFileError(fileInfo *FileInfo, errMessage string) {
	log.Println(errMessage)
	fileInfo.IsError = true
	fileInfo.SourceUrl += errMessage
}

func fetchFileWorker() {
	var netTransport = &http.Transport{
		Dial: (&net.Dialer{
			Timeout: 20 * time.Second,
		}).Dial,
		TLSHandshakeTimeout: 20 * time.Second,
	}
	var httpClient = &http.Client{
		Timeout:   0,
		Transport: netTransport,
	}
	for fileInfo := range fileTasks {
		sourceUrl := fileInfo.SourceUrl
		if testfileFilenameRegexp.MatchString(fileInfo.FileName) {
			handleFetchFileError(fileInfo, "refused to download testfile:")
			continue
		}
		switch {
		case strings.HasPrefix(sourceUrl, "http"):
			fileInfo = fetchHTTPContent(httpClient, fileInfo)
		case strings.HasPrefix(sourceUrl, "magnet:?xt=urn:btih:"):
			fileInfo = fetchMagnetContent(fileInfo)
		default:
			// 既不是http 也不是magnet
			handleFetchFileError(fileInfo, "not support this protocol,sourceUrl:")
			continue
		}
		// finish download pushFilesUpdate fileInfo
		fileInfo.Duration = time.Now().Unix() - fileInfo.StartTimeStamp
		if fileInfo.Duration <= 0 {
			fileInfo.Duration = 1
		}
		if fileInfo.ContentLength > 0 {
			fileInfo.Speed = fileInfo.ContentLength / fileInfo.Duration
			fileInfo.Size = fileInfo.ContentLength
		} else {
			sysFileInfo, err := os.Stat(DOWNLOAD_DIRNAME + "/" + fileInfo.FileName)
			if err != nil {
				handleFetchFileError(fileInfo, err.Error())
				continue
			}
			fileInfo.Size = sysFileInfo.Size()
			fileInfo.ContentLength = fileInfo.Size
			fileInfo.Speed = fileInfo.Size / fileInfo.Duration
		}
		fileInfo.IsDownloaded = true
		// 下载完成后, 推送文件信息
		pushFilesUpdate <- struct{}{}
	}
}

func fetchHTTPContent(httpClient *http.Client, fileInfo *FileInfo) *FileInfo {
	// http
	fileInfo.StartTimeStamp = time.Now().Unix()
	resp, err := httpClient.Get(fileInfo.SourceUrl)
	if err != nil {
		handleFetchFileError(fileInfo, "http.Client error"+err.Error())
		return fileInfo
	}
	fileInfo.ContentLength = resp.ContentLength
	contentDisposition := strings.SplitN(resp.Header.Get("Content-Disposition"), "=", 2)
	var attachmentName string
	if len(contentDisposition) > 1 {
		attachmentName = contentDisposition[1]
	}
	if fileInfo.ContentLength <= 0 {
		//一些资源是动态生成的,请求第一次是chunked stream,Header不带Content-Length,第二次请求就有Content-length
		resp, err := httpClient.Get(fileInfo.SourceUrl)
		if err != nil {
			handleFetchFileError(fileInfo, "http.Client error"+err.Error())
			return fileInfo
		}
		fileInfo.ContentLength = resp.ContentLength
	}
	// if header has attach filename, update it
	if attachmentName != "" {
		attachmentName = getSafeFilename(attachmentName)
		fileInfos.mutex.Lock()
		fileInfos.infos[attachmentName] = &FileInfo{
			FileName:       attachmentName,
			SourceUrl:      fileInfo.SourceUrl,
			Size:           fileInfo.Size,
			ContentLength:  fileInfo.ContentLength,
			StartTimeStamp: fileInfo.StartTimeStamp,
			Duration:       fileInfo.Duration,
			Speed:          fileInfo.Speed,
			IsDownloaded:   fileInfo.IsDownloaded,
			IsError:        fileInfo.IsError,
		}
		delete(fileInfos.infos, fileInfo.FileName)
		fileInfo = fileInfos.infos[attachmentName]
		fileInfos.mutex.Unlock()
	}
	log.Printf("Create Download: length:%s source:%s filename:%s \n", getHumanSizeString(fileInfo.ContentLength), fileInfo.SourceUrl, fileInfo.FileName)
	if fileInfo.ContentLength > LIMIT_SIZE {
		handleFetchFileError(fileInfo, "The content length of sourceUrl is too big :")
		return fileInfo
	}
	// write file
	fp, err := os.Create(DOWNLOAD_DIRNAME + "/" + fileInfo.FileName)
	if err != nil {
		errMessage := fmt.Sprintf("create file error %v\n", err)
		handleFetchFileError(fileInfo, errMessage)
		return fileInfo
	}
	bufSize := 4096
	bodyReader := bufio.NewReaderSize(resp.Body, bufSize)
	if err != nil {
		errMessage := fmt.Sprintf("create file error %v\n", err)
		handleFetchFileError(fileInfo, errMessage)
		return fileInfo
	}
	buf := make([]byte, bufSize)
	size := 0
	readSize := 0
	for i := 0; ; i++ {
		readSize, err = bodyReader.Read(buf)
		if err != nil {
			if err == io.EOF {
				fp.Write(buf[:readSize])
				break
			} else {
				errMessage := fmt.Sprintf("body read err%v\n", err)
				handleFetchFileError(fileInfo, errMessage)
				break
			}
		}
		_, err = fp.Write(buf[:readSize])
		size += readSize
		if i%1000 == 0 {
			fileInfo.Size = int64(size)
			duration := time.Now().Unix() - fileInfo.StartTimeStamp
			if duration == 0 {
				duration = 1
			}
			fileInfo.Speed = fileInfo.Size / duration
		}
		if err != nil {
			errMessage := fmt.Sprintf("body write err:%v\n", err)
			handleFetchFileError(fileInfo, errMessage)
			break
		}
	}
	fp.Close()
	resp.Body.Close()
	return fileInfo
}

func fetchMagnetContent(fileInfo *FileInfo) *FileInfo {
	//support magnet
	// check aria2c
	if !isAria2cRunning {
		errMessage := "aria2c is not running,cannot download magnet"
		handleFetchFileError(fileInfo, errMessage)
		return fileInfo
	}
	//send json rpc
	aria2TaskId := fileInfo.FileName
	response, err := rpcCallAria2c(ARIA2_ADD_URL_METHOD, aria2TaskId, []interface{}{[]string{fileInfo.SourceUrl}})
	if err != nil {
		errMessage := fmt.Sprintf("rpc_call error when calling aria2.addUrl %v source_url:", err)
		handleFetchFileError(fileInfo, errMessage)
		return fileInfo
	}
	taskGid := response.Result
	fileInfo.StartTimeStamp = time.Now().Unix()
	// get task info
	taskStatus := "active"
	updateFileName := false
	for taskStatus != "complete" {
		time.Sleep(time.Second * 5)
		response, err = rpcCallAria2c(ARIA2_TELL_STATUS_METHOD, aria2TaskId, []interface{}{taskGid})
		if err != nil {
			errMessage := fmt.Sprintf("rpc_call error when calling aria2.tellStatus %v source_url:", err)
			handleFetchFileError(fileInfo, errMessage)
			return fileInfo
		}
		result := response.Result.(map[string]interface{})
		taskStatus = result["status"].(string)
		// check error message
		resultErrorMessage := result["errorMessage"]
		if !(resultErrorMessage == nil || resultErrorMessage == "") {
			errMessage := fmt.Sprintf("aria2 error:%v source_url:", resultErrorMessage)
			handleFetchFileError(fileInfo, errMessage)
			return fileInfo
		}
		//arai2c 返回的totalLength的很奇怪,单位不是byte?? 如有大佬知道,还望告知,谢谢
		//totalLength := result["totalLength"].(string)
		//fileInfo.ContentLength, _ = strconv.ParseInt(totalLength, 10, 64)
		// 磁力链接建立任务时无法指定文件名 获得真实文件名后需要重命名
		if !updateFileName {
			realFilename := strings.Replace(result["files"].([]interface{})[0].(map[string]interface{})["path"].(string), "[METADATA]", "", 1)
			//检查同名文件以下载同名文件以免覆盖已下载文件
			fileInfos.mutex.Lock()
			if fileInfos.infos[realFilename] != nil {
				errMessage := fmt.Sprintf("file %v is exist. source_url:", realFilename)
				handleFetchFileError(fileInfo, errMessage)
				return fileInfo
			}
			fileInfos.infos[realFilename] = &FileInfo{
				FileName:       realFilename,
				SourceUrl:      fileInfo.SourceUrl,
				Size:           fileInfo.Size,
				ContentLength:  fileInfo.ContentLength,
				StartTimeStamp: fileInfo.StartTimeStamp,
				Duration:       fileInfo.Duration,
				Speed:          fileInfo.Speed,
				IsDownloaded:   fileInfo.IsDownloaded,
				IsError:        fileInfo.IsError,
			}
			delete(fileInfos.infos, fileInfo.FileName)
			fileInfo = fileInfos.infos[realFilename]
			fileInfos.mutex.Unlock()
			updateFileName = true
		}
		resultJson, _ := json.Marshal(response.Result)
		log.Printf("aria2 status:%s\n", resultJson)
		// update fileInfo.size
		sysFileInfo, err := os.Stat(DOWNLOAD_DIRNAME + "/" + fileInfo.FileName)
		if err != nil {
			handleFetchFileError(fileInfo, err.Error())
			continue
		}
		fileInfo.Size = sysFileInfo.Size()
	}
	//aria2.removeDownloadResult
	response, err = rpcCallAria2c(ARIA2_REMOVE_DOWNLOAD_RESULT, aria2TaskId, []interface{}{taskGid})
	if err != nil {
		log.Printf("rpc_call error when calling aria2.removeDownloadResult %v\n", err)
	}
	return fileInfo
}

//utils
func getSafeFilename(url string) string {
	_, filenameInUrl := path.Split(url)
	filename := strings.Join(safeFilenameRegexp.FindAllString(filenameInUrl, -1), "")
	if lenOfFilename := len(filename); lenOfFilename > 50 {
		filename = filename[lenOfFilename-50 : lenOfFilename]
	}
	fileExt := path.Ext(filename)
	return fmt.Sprintf("%s-%v%s", strings.Replace(filename, fileExt, "", -1), time.Now().Unix(), fileExt)

}
func getHumanSizeString(byteSize int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "EB"}
	index := 0
	byteSizeFloat := float64(byteSize)
	for ; byteSizeFloat > 1024; index += 1 {
		byteSizeFloat /= 1024
	}
	return fmt.Sprintf("%.2f %s", byteSizeFloat, units[index])
}
func hasAria2c() bool {
	output, _ := exec.Command("hash", "aria2c").Output()
	if len(output) == 0 {
		return true
	}
	return false
}
func rpcCallAria2c(method string, id string, params []interface{}) (*Aria2JsonRPCResp, error) {
	var response Aria2JsonRPCResp
	rpcRequests, err := json.Marshal(Aria2JsonRPCReq{Method: method, Jsonrpc: "2.0", Id: id, Params: params})
	if err != nil {
		log.Printf("json marshal error %v %s\n", err, rpcRequests)
		return &response, err
	}
	rpcResponse, err := http.Post("http://127.0.0.1:6900/jsonrpc", "application/json-rpc", bytes.NewReader(rpcRequests))
	if err != nil {
		log.Println("jsonrpc call error", err.Error())
		return &response, err
	}
	defer rpcResponse.Body.Close()
	rpcBodies, err := ioutil.ReadAll(rpcResponse.Body)
	if err != nil {
		log.Println("jsonrpc response read error", err.Error())
		return &response, err
	}
	err = json.Unmarshal(rpcBodies, &response)
	if err != nil {
		log.Printf("json unmarshal error %v %s\n", err, rpcBodies)
		return &response, err
	}
	return &response, err
}
func main() {
	//make dir and init
	os.Mkdir("download", 0777)
	listFiles(DOWNLOAD_DIRNAME)
	// fileInfos watcher worker
	go func() {
		flag := false
		for {
			time.Sleep(time.Second)
			// 当有文件在下载时, 推送文件信息
			fileInfos.mutex.RLock()
			for _, fileInfo := range fileInfos.infos {
				if !fileInfo.IsDownloaded {
					flag = true
					break
				}
			}
			fileInfos.mutex.RUnlock()
			if flag {
				pushFilesUpdate <- struct{}{}
				flag = false
			}
		}
	}()
	// push files stat worker
	go func() {
		for {
			connections.mutex.Lock()
			for conn := range connections.conns {
				err := conn.WriteJSON(fileInfos.infos)
				if err != nil {
					delete(connections.conns, conn)
					log.Println("connection close", conn.RemoteAddr())
					conn.Close()
					log.Printf("number of active connections %v\n", len(connections.conns))
				}
			}
			connections.mutex.Unlock()
			<-pushFilesUpdate
		}
	}()
	// running aria2c with enable rpc once
	if hasAria2c() && !isAria2cRunning {
		go func() {
			isAria2cRunning = true
			cmd := exec.Command("aria2c", "--dir=download", "--enable-rpc", "--rpc-listen-port=6900", "--rpc-listen-all=false")
			err := cmd.Start()
			if err != nil {
				log.Println("aria2c can not start :", err.Error())
				isAria2cRunning = false
			}
			time.Sleep(0)
			cmd.Wait()
		}()
	} else {
		log.Println("aria2c not install,cannot download magnet")
	}
	//http server
	//http.Handle("/static/", http.StripPrefix("/static", http.FileServer(http.Dir("static/"))))
	http.Handle("/download/", http.StripPrefix("/download", http.FileServer(http.Dir("download"))))
	http.HandleFunc("/file_download_proxy/files", filesInfoHandler)
	http.HandleFunc("/file_download_proxy/file", fileOperationHandler)
	http.HandleFunc("/favicon.ico", func(w http.ResponseWriter, req *http.Request) {
		http.ServeFile(w, req, "favicon.ico")
	})
	http.HandleFunc("/file_download_proxy/", func(w http.ResponseWriter, req *http.Request) {
		w.Write(indexData.Bytes())
	})
	log.Printf("service start at %v", bindAddr)
	log.Fatal(http.ListenAndServe(bindAddr, nil))
}
