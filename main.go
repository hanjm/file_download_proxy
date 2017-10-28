package main

import (
	"flag"
	"github.com/hanjm/log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	var (
		port                = flag.Int("port", 8080, "service listen port")
		downloadDir         = flag.String("dir", "download", "download dir")
		fileSizeLimitGB     = flag.Int64("limit", 5, "the limit size of download file, unit is 'GB'")
		downloadTimeoutHour = flag.Int64("timeout", 48, "the limit time for finish download task, unit is 'Hour'")
		basicAuth           = flag.String("auth", "", "http basic access authentication, username:password")
	)
	// 处理flag
	flag.Parse()
	err := os.MkdirAll(*downloadDir, 0777)
	if err != nil && !os.IsExist(err) {
		log.Fatalf("fail to create download dir:%s, err:%s", *downloadDir, err)
	}
	tasksManager := NewTasksManager(*downloadDir, *fileSizeLimitGB*1024*1024*1024, time.Duration(*downloadTimeoutHour)*time.Hour)
	err = tasksManager.RestoreFromJSON()
	if err != nil {
		log.Errorf("tasksManager.RestoreFromJSON error:%s", err)
	}
	tasksManager.ListFiles()
	// http server
	go HTTPServer(tasksManager, *port, *basicAuth)
	// aria2 worker
	pid := Aria2Worker(*downloadDir)
	log.Infof("aria2c pid is %d", pid)
	defer syscall.Kill(pid, syscall.SIGQUIT)
	// ReDownloadUncompleted task
	tasksManager.ReDownloadUncompleted()
	// push download tasks info update worker
	go tasksManager.PushTasksUpdateWorker()
	// signal SIGHUP reload index.html
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGSTOP, syscall.SIGUSR1, syscall.SIGUSR2)
	for {
		s := <-c
		log.Infof("received signal %s", s)
		switch s {
		case syscall.SIGUSR1:
		case syscall.SIGUSR2:
			err := tasksManager.BackupToJSON()
			if err != nil {
				log.Fatalf("tasksManager.BackupToJSON error:%s", err.Error())
			}
			err = tasksManager.RestoreFromJSON()
			if err != nil {
				log.Errorf("tasksManager.RestoreFromJSON error:%s", err)
			}
			tasksManager.ListFiles()
		case syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGSTOP:
			err := tasksManager.BackupToJSON()
			if err != nil {
				log.Fatalf("tasksManager.BackupToJSON error:%s", err.Error())
			}
			return
		}
	}
}
