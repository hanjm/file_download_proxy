package main

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/hanjm/log"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"
	//"syscall"
	"net/url"
)

type TasksManager struct {
	tasks               []Task
	ConnectionsManger   *ConnectionsManger
	downloadDir         string
	limitByteSize       int64
	limitTimeout        time.Duration
	PushTasksUpdateChan chan struct{}
}

func NewTasksManager(downloadDir string, limitByteSize int64, limitTimeout time.Duration) *TasksManager {
	return &TasksManager{
		tasks:               make([]Task, 0, 64),
		ConnectionsManger:   NewConnectionsManger(),
		PushTasksUpdateChan: make(chan struct{}, 1),
		downloadDir:         downloadDir,
		limitByteSize:       limitByteSize,
		limitTimeout:        limitTimeout,
	}
}

func (m *TasksManager) WebSocketHandler(w http.ResponseWriter, r *http.Request) {
	var upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Errorf("websocket upgrader.Upgrade error:%s", err)
		return
	}
	log.Infof("new connection from %s, number of active connections %d", conn.RemoteAddr(), len(m.ConnectionsManger.conns)+1)
	m.ConnectionsManger.Add(conn)
	// 首次连接,推送文件信息
	m.PushTasksUpdateChan <- struct{}{}
}

func (m *TasksManager) TaskHandler(w http.ResponseWriter, r *http.Request) {
	// filename有特殊字符如&时无法正常通过r.URL.Query()获取
	filename := strings.TrimPrefix(r.URL.RawQuery, "filename=")
	filename, err := url.QueryUnescape(filename)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("param filename is invalid:" + r.URL.RawQuery))
		return
	}
	filename = strings.Replace(filename, "/", "", -1)
	switch r.Method {
	case http.MethodGet:
		log.Infof("[TaskHandler]download %s", filename)
		// 下载文件
		if filename == "" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("param filename is empty"))
			return
		}
		http.Redirect(w, r, m.downloadDir+filename, http.StatusTemporaryRedirect)
	case http.MethodPost:
		// 新建任务
		sourceURL := strings.TrimSpace(r.PostFormValue("url"))
		log.Infof("[TaskHandler]create task:%s", sourceURL)
		if sourceURL == "" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("param url is empty"))
			return
		}
		// check total size
		filesSize := m.ListFiles()
		if filesSize > m.limitByteSize {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(fmt.Sprintf("There are too many files in server, please delete some files, FilesSize:%s", getHumanSizeString(filesSize))))
			return
		}
		task, err := NewDownloadTask(sourceURL)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(err.Error()))
			return
		}
		m.AddTask(task)
		go func(m *TasksManager) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Errorf("download worker panic:%s", rec)
				}
			}()
			err := task.Download(m.downloadDir, m.limitByteSize, m.limitTimeout)
			if err != nil {
				log.Errorf("task download error:%s, task name:%s", err, task.FileName())
			}
			m.PushTasksUpdateChan <- struct{}{}
		}(m)
		// 添加任务后,推送文件信息
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("CREATE OK"))
		m.PushTasksUpdateChan <- struct{}{}
		return
	case http.MethodDelete:
		log.Infof("[TaskHandler]delete %s", filename)
		// 删除文件
		defer func() {
			m.PushTasksUpdateChan <- struct{}{}
		}()
		if filename == "" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("param filename is empty"))
			return
		}
		// 正在下载不能删
		if task := m.GetTask(filename); task != nil && task.IsCompleted() {
			w.WriteHeader(http.StatusBadRequest)
			log.Infof("[TaskHandler]delete fail,task is downloading %s", filename)
			w.Write([]byte("task is downloading"))
			return
		}
		err := m.RemoveTask(filename)
		if err != nil {
			log.Errorf("delete '%s' error:%s", filename, err)
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(fmt.Sprintf("delete error:%s", err)))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("DELETE OK"))
		log.Infof("[TaskHandler]delete ok, %s", filename)
		m.PushTasksUpdateChan <- struct{}{}
	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

// HasDownloadingTask 检查是否有正在下载的任务
func (m *TasksManager) HasDownloadingTask() bool {
	for _, v := range m.tasks {
		if !v.IsCompleted() {
			return true
		}
	}
	return false
}

func (m *TasksManager) GetTasks() []Task {
	return m.tasks
}

func (m *TasksManager) GetTask(filename string) Task {
	for _, v := range m.tasks {
		if v.FileName() == filename {
			return v
		}
	}
	return nil
}

func (m *TasksManager) AddTask(t Task) {
	m.tasks = append(m.tasks, t)
}

func (m *TasksManager) RemoveTask(filename string) error {
	temp := make([]Task, 0, len(m.tasks))
	for _, v := range m.tasks {
		if v.FileName() != filename {
			temp = append(temp, v)
		}
	}
	m.tasks = temp
	err := os.RemoveAll(m.downloadDir + "/" + filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return nil
}

// backup and restore
const backupFilename = "tasks.json"

func (m *TasksManager) BackupToJSON() error {
	data, err := json.Marshal(m.tasks)
	if err != nil {
		return fmt.Errorf("json.Marshal error:%s", err)
	}
	os.Remove(backupFilename)
	fp, err := os.Create(backupFilename)
	if err != nil {
		return fmt.Errorf("os.Create error:%s", err)
	}
	defer fp.Close()
	defer fp.Sync()
	_, err = fp.Write(data)
	if err != nil {
		return fmt.Errorf("fp.Write error:%s", err)
	}
	return nil
}

func (m *TasksManager) RestoreFromJSON() error {
	fileData, err := ioutil.ReadFile(backupFilename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("ReadFile error:%s", err)
	}
	var httpTasks []*HTTPTask
	err = json.Unmarshal(fileData, &httpTasks)
	if err != nil {
		return fmt.Errorf("json.Unmarshal error:%s", err)
	}
	for _, ht := range httpTasks {
		// 删除不存在的
		if _, err := os.Stat(fmt.Sprintf("%s/%s", m.downloadDir, ht.TaskInfo.FileName)); err != nil && os.IsNotExist(err) {
			continue
		}
		switch ht.TaskType {
		case DownloadTaskTypeHTTP:
			m.AddTask(ht)
		case DownloadTaskTypeMagnet:
			mt := &MagnetTask{
				TaskType: DownloadTaskTypeMagnet,
				TaskInfo: ht.TaskInfo,
			}
			m.AddTask(mt)
		}
	}
	return nil
}

// 如果有未完成的, 继续下载
func (m *TasksManager) ReDownloadUncompleted() {
	for _, task := range m.tasks {
		if !task.IsCompleted() {
			log.Infof("ReDownloadUncompleted task:%s", task.FileName())
			go func(m *TasksManager) {
				defer func() {
					if rec := recover(); rec != nil {
						log.Errorf("download worker panic:%s", rec)
					}
				}()
				os.Remove(m.downloadDir + "/" + task.FileName())
				err := task.Download(m.downloadDir, m.limitByteSize, m.limitTimeout)
				if err != nil {
					log.Errorf("task download error:%s, task name:%s", err, task.FileName())
				}
				m.PushTasksUpdateChan <- struct{}{}
			}(m)
		}
	}
}
func (m *TasksManager) ListFiles() (fileTotalSize int64) {
	files, _ := ioutil.ReadDir(m.downloadDir)
	for _, file := range files {
		filename := file.Name()
		if strings.HasSuffix(filename, ".torrent") ||
			strings.HasSuffix(filename, ".aria2") {
			continue
		}
		task := m.GetTask(filename)
		if task == nil {
			//rebuild new local file
			fileSize := file.Size()
			newLocalTask := NewHTTPTask("Local")
			newLocalTask.TaskInfo.FileName = filename
			newLocalTask.TaskInfo.Size = fileSize
			newLocalTask.TaskInfo.ContentLength = fileSize
			// todo: syscall.Stat_t is different between linux and macos
			//if fs, ok := file.Sys().(syscall.Stat_t); ok {
			//	newLocalTask.TaskInfo.StartTime = time.Unix(fs.Ctimespec.Sec, fs.Ctimespec.Nsec)
			//	if delta := fs.Mtimespec.Sec - fs.Ctimespec.Sec; delta > 0 {
			//		newLocalTask.TaskInfo.Duration = delta
			//		newLocalTask.Speed = fileSize / delta
			//	}
			//} else {
			//	newLocalTask.TaskInfo.StartTime = file.ModTime()
			//}
			newLocalTask.TaskInfo.StartTime = file.ModTime()
			newLocalTask.TaskInfo.IsCompleted = true
			m.AddTask(newLocalTask)
			fileTotalSize += file.Size()
		}
	}
	return fileTotalSize
}

func (m *TasksManager) PushTasksUpdateWorker() {
	defer func() {
		if rec := recover(); rec != nil {
			log.Errorf("pushTasksUpdateWorker panic:%s", rec)
		}
	}()
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Errorf("pushTasksUpdateWorker panic:%s", rec)
			}
		}()
		for {
			select {
			case <-m.PushTasksUpdateChan:
				log.Debugf("m.PushTasksUpdateChan received")
				m.ConnectionsManger.Broadcast(m.GetTasks())
			}
		}
	}()
	for {
		ticker := time.NewTicker(time.Second)
		select {
		case <-ticker.C:
			if m.HasDownloadingTask() && m.ConnectionsManger.Count() > 0 {
				// 当有文件在下载且有连接时, 推送下载任务更新信息
				m.PushTasksUpdateChan <- struct{}{}
			}
		}
	}
}
