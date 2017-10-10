package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"github.com/hanjm/log"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strings"
	"time"
)

type Task interface {
	Download(downloadDir string, limitByteSize int64, limitTimeout time.Duration) error
	IsCompleted() bool
	FileName() string
	ContentLength() int64
}

func NewDownloadTask(sourceURL string) (Task, error) {
	switch {
	case strings.HasPrefix(sourceURL, "http"):
		return NewHTTPTask(sourceURL), nil
	case strings.HasPrefix(sourceURL, "magnet:?xt=urn:btih:") || IsBase64String(sourceURL):
		return NewMagnetTask(sourceURL), nil
	default:
		return nil, fmt.Errorf("sourceURL expect http or magnet, not %s", sourceURL)
	}
}

const (
	DownloadTaskTypeHTTP = iota
	DownloadTaskTypeMagnet
)

type TaskInfo struct {
	SourceURL     string
	StartTime     time.Time
	FileName      string
	ContentLength int64         // B 总大小
	Size          int64         // B 已下载的大小
	Duration      time.Duration // s 耗时
	Speed         int64         // B/s 速度
	IsCompleted   bool          // 是否完成
	IsError       bool          // 是否出错
	Error         string        // 错误消息
}

// download http content
type HTTPTask struct {
	TaskType int
	TaskInfo
}

func NewHTTPTask(sourceUrl string) *HTTPTask {
	return &HTTPTask{
		TaskType: DownloadTaskTypeHTTP,
		TaskInfo: TaskInfo{
			SourceURL: sourceUrl,
			FileName:  getSafeFilename(sourceUrl),
		}}
}

func (t *HTTPTask) Download(downloadDir string, limitByteSize int64, limitTimeout time.Duration) error {
	var httpClient = &http.Client{
		Timeout: limitTimeout,
		Transport: &http.Transport{
			Dial: (&net.Dialer{
				Timeout: 20 * time.Second,
			}).Dial,
			TLSHandshakeTimeout: 20 * time.Second,
		},
	}
	t.StartTime = time.Now()
	resp, err := httpClient.Get(t.SourceURL)
	if err != nil {
		return t.Errorf("http.Client error:%s", err)
	}
	defer resp.Body.Close()
	t.TaskInfo.ContentLength = resp.ContentLength
	contentDisposition := strings.SplitN(resp.Header.Get("Content-Disposition"), "=", 2)
	var attachmentName string
	if len(contentDisposition) > 1 {
		attachmentName = contentDisposition[1]
	}
	if t.TaskInfo.ContentLength <= 0 {
		resp.Body.Close()
		//一些资源是动态生成的,请求第一次是chunked stream,Header不带Content-Length,第二次请求就有Content-length
		resp, err = httpClient.Get(t.SourceURL)
		if err != nil {
			return t.Errorf("http.Client error:%s", err)
		}
		t.TaskInfo.ContentLength = resp.ContentLength
	}
	// if header has attach filename, update it
	if attachmentName != "" {
		attachmentName2 := getSafeFilename(attachmentName)
		if attachmentName2 != t.TaskInfo.FileName {
			t.TaskInfo.FileName = attachmentName2
		}
	}
	log.Infof("create HTTP task: length:%s source:%s filename:%s", getHumanSizeString(t.TaskInfo.ContentLength), t.SourceURL, t.TaskInfo.FileName)
	if t.TaskInfo.ContentLength > limitByteSize {
		return t.Errorf("the content length of sourceUrl is too big:%d, limit:%d", t.TaskInfo.ContentLength, limitByteSize)
	}
	// write file
	filename := downloadDir + "/" + t.TaskInfo.FileName
	os.Remove(filename)
	fp, err := os.Create(filename)
	if err != nil {
		return t.Errorf("create file error:%s", err)
	}
	defer fp.Sync()
	defer fp.Close()
	bufSize := 4096
	bodyReader := bufio.NewReaderSize(resp.Body, bufSize)
	if err != nil {
		return t.Errorf("create file error:%s", err)
	}
	buf := make([]byte, bufSize)
	size := 0
	readSize := 0
	completed := false
	for i := 0; ; i++ {
		readSize, err = bodyReader.Read(buf)
		if err != nil {
			if err == io.EOF {
				// 正常下载完
				completed = true
			} else {
				return t.Errorf("body read error:%s", err)
			}
		}
		_, err = fp.Write(buf[:readSize])
		size += readSize
		t.Size = int64(size)
		if i%1000 == 0 {
			t.Duration = time.Now().Sub(t.StartTime)
			t.Speed = calculateDownloadSpeed(t.Size, t.Duration)
		}
		if err != nil {
			return t.Errorf("body write error:%s", err)
		}
		if completed {
			break
		}
	}
	t.TaskInfo.IsCompleted = true
	t.Size = int64(size)
	t.TaskInfo.ContentLength = t.Size
	t.Duration = time.Now().Sub(t.StartTime)
	t.Speed = calculateDownloadSpeed(t.Size, t.Duration)
	log.Infof("complete HTTP task: length:%s source:%s filename:%s, duration:%s", getHumanSizeString(t.TaskInfo.ContentLength), t.SourceURL, t.TaskInfo.FileName, t.Duration)
	return nil
}

func (t *HTTPTask) IsCompleted() bool {
	return t.TaskInfo.IsCompleted
}

func (t *HTTPTask) FileName() string {
	return t.TaskInfo.FileName
}

func (t *HTTPTask) ContentLength() int64 {
	return t.TaskInfo.ContentLength
}

func (t *HTTPTask) Errorf(format string, a ...interface{}) (err error) {
	err = fmt.Errorf(format, a...)
	_, file, line, ok := runtime.Caller(1)
	if ok {
		log.Errorf("[%s:%d]%s", file, line, err.Error())
	}
	t.TaskInfo.IsError = true
	t.TaskInfo.IsCompleted = true
	t.TaskInfo.Error = err.Error()
	return err
}

// download magnet content
type MagnetTask struct {
	TaskType int
	TaskInfo
}

func NewMagnetTask(sourceUrl string) *MagnetTask {
	return &MagnetTask{
		TaskType: DownloadTaskTypeMagnet,
		TaskInfo: TaskInfo{
			SourceURL: sourceUrl,
			FileName:  getSafeFilename(sourceUrl),
		}}
}
func (t *MagnetTask) Download(downloadDir string, limitByteSize int64, limitTimeout time.Duration) (err error) {
	if !IsAria2cRunning() {
		return t.Errorf("aria2c is not running, cannot download magnet")
	}
	aria2cRPCClient := NewAria2cRPCClient()
	var taskGID string
	// magnet? / torrent? / torrent file in downloadDir
	var isMagnetLink bool
	var torrentBase64 string
	data, err := base64.StdEncoding.DecodeString(t.SourceURL)
	if err != nil {
		if data, err = ioutil.ReadFile(t.SourceURL); err != nil {
			isMagnetLink = true
		} else {
			// try read from torrent file in downloadDir, for reDownload torrent
			isMagnetLink = false
			torrentBase64 = base64.StdEncoding.EncodeToString(data)
		}
	} else {
		isMagnetLink = false
		torrentBase64 = t.SourceURL
	}
	if isMagnetLink {
		taskGID, err = aria2cRPCClient.AddURI(t.SourceURL)
		if err != nil {
			return t.Errorf("call aria2c AddURI error:%s", err)
		}
	} else {
		taskGID, err = aria2cRPCClient.AddTorrent(torrentBase64)
		if err != nil {
			return t.Errorf("call aria2c AddTorrent error:%s", err)
		}
		// save to file and change the sourceURL
		torrentFilename := fmt.Sprintf("%s/%s.torrent", downloadDir, torrentBase64[:16])
		fp, err := os.Create(torrentFilename)
		if err != nil {
			log.Warnf("os.Create file error:%s", err)
		}
		_, err = fp.Write(data)
		if err != nil {
			log.Warnf("fp.Write error:%s", err)
		}
		t.SourceURL = torrentFilename
	}
	log.Infof("create Magnet task: sourceURL:%s, taskGID:%s", t.SourceURL, taskGID)
	t.StartTime = time.Now()
	timeout := limitTimeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
MagnetLoop:
	complete := false
	ticker := time.NewTicker(time.Second * 5)
	for !complete {
		select {
		case <-ticker.C:
			result, err := aria2cRPCClient.TellStatus(taskGID)
			if err != nil {
				return t.Errorf("call aria2c TellStatus error:%s", err)
			}
			// update break condition
			complete = result.Completed()
			// udpate taskInfo
			t.TaskInfo.ContentLength = result.TotalLength
			t.Size = result.CompletedLength
			t.Duration = time.Now().Sub(t.StartTime)
			t.Speed = result.DownloadSpeed
			if !complete && result.CompletedLength > 0 && result.CompletedLength == result.TotalLength {
				// why aria2c wait so long time even it seems the download task is completed, may be as a seeder?
				log.Infof("force to set task status complete, status:%+v", result)
				time.Sleep(time.Second * 5)
				log.Infof("force to set task status complete, status:%+v", result)
				complete = true
			}
			// 磁力链接建立任务时无法指定文件名 获得真实文件名后需要重命名
			realFilename := strings.TrimPrefix(result.GetFilePath(), downloadDir+"/")
			pos := strings.Index(realFilename, "/")
			if pos != -1 && pos < len(realFilename)-1 {
				realFilename = realFilename[:pos]
			}
			t.TaskInfo.FileName = realFilename
			// 检查是否有继续下载磁力链接包含的其他文件
			followedBys := result.FollowedBy
			for _, followedTaskGID := range followedBys {
				taskGID = followedTaskGID
				complete = false
				log.Debugf("goto MagnetLoop: task status:%+v", result)
				goto MagnetLoop
			}
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				return t.Errorf("task timeout:%s", timeout)
			}
		}
	}
	err = aria2cRPCClient.RemoveDownloadResult(taskGID)
	if err != nil {
		log.Warnf("RemoveDownloadResult error:%s", err)
	}
	t.TaskInfo.IsCompleted = true
	t.Duration = time.Now().Sub(t.StartTime)
	t.Speed = calculateDownloadSpeed(t.Size, t.Duration)
	return nil
}

func (t *MagnetTask) IsCompleted() bool {
	return t.TaskInfo.IsCompleted
}

func (t *MagnetTask) FileName() string {
	return t.TaskInfo.FileName
}

func (t *MagnetTask) ContentLength() int64 {
	return t.TaskInfo.ContentLength
}

func (t *MagnetTask) Errorf(format string, a ...interface{}) (err error) {
	err = fmt.Errorf(format, a...)
	_, file, line, ok := runtime.Caller(1)
	if ok {
		log.Errorf("[%s:%d]%s", file, line, err.Error())
	}
	t.TaskInfo.IsError = true
	t.TaskInfo.IsCompleted = true
	t.TaskInfo.Error = err.Error()
	return err
}

//utils
var safeFilenameRegexp = regexp.MustCompile(`[\w\d\-.]+`)

func getSafeFilename(str string) string {
	filename := strings.Join(safeFilenameRegexp.FindAllString(str, -1), "")
	if lenOfFilename := len(filename); lenOfFilename > 50 {
		filename = filename[lenOfFilename-50 : lenOfFilename]
	}
	return fmt.Sprintf("%d-%s", time.Now().Unix(), filename)
}

func getHumanSizeString(byteSize int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "EB"}
	index := 0
	byteSizeFloat := float64(byteSize)
	for ; byteSizeFloat > 1024; index += 1 {
		byteSizeFloat /= 1024
	}
	var unit string
	if index < len(units) {
		unit = units[index]
	} else {
		unit = "INF"
	}
	return fmt.Sprintf("%.2f %s", byteSizeFloat, unit)
}

func calculateDownloadSpeed(byteSize int64, duration time.Duration) (bytePerSecond int64) {
	if duration <= 0 {
		duration = 1
	}
	return byteSize * 1e9 / int64(duration)
}

func IsBase64String(str string) bool {
	_, err := base64.StdEncoding.DecodeString(str)
	if err != nil {
		return false
	}
	return true
}
