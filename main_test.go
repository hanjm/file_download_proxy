package main

import (
	"testing"
	"time"
	"github.com/hanjm/log"
	"sync"
	"github.com/gorilla/websocket"
)

func TestTasksManager_WebSocketHandler(t *testing.T) {
	// initTestEnv
	const downloadDir = "download"
	const limitByteSize = 3 * 1024 * 1024 * 1024
	const limitTimeout = time.Hour * 24
	tasksManager := NewTasksManager(downloadDir, limitByteSize, limitTimeout)
	go HTTPServer(tasksManager, "127.0.0.1:8081", 8081, "")
	go tasksManager.PushTasksUpdateWorker()
	// build 10 tasks
	var wg sync.WaitGroup
	wg.Add(10)
	for i := 0; i < 10; i++ {
		task := NewHTTPTask("https://github.com/hashicorp/consul/archive/v1.0.0-beta2.tar.gz")
		tasksManager.AddTask(task)
		time.Sleep(time.Second)
		go func(i int) {
			defer wg.Done()
			log.Infof("initServerTask:%d", i)
			task.Download(downloadDir, limitByteSize, limitTimeout)
		}(i)
	}
	// client
	for i := 0; i < 1000; i++ {
		go func(i int) {
			var err error
			log.Infof("initClient %d", i)
			conn, _, err := websocket.DefaultDialer.Dial("ws://127.0.0.1:8081/file_download_proxy/ws", nil)
			if err != nil {
				log.Errorf("websocket dail error:%s", err)
			}
			//var msg []byte
			for {
				_, _, err = conn.ReadMessage()
				//log.Debugf("client %d received msg:%s", i, msg)
				if err != nil {
					break
				}
			}
			log.Infof("client %d closed", i)
		}(i)
	}
	wg.Wait()
}
