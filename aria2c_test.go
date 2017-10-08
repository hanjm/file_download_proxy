package main

import (
	"flag"
	"github.com/hanjm/log"
	"syscall"
	"testing"
	"time"
)

func withTestEnv(fn func()) {
	flag.Parse()
	pid := Aria2Worker("download")
	log.Infof("aria2c pid is %d", pid)
	time.Sleep(time.Second)
	defer syscall.Kill(pid, syscall.SIGQUIT)
	fn()
}

func TestAria2cRPCClient_AddURI(t *testing.T) {
	t.Run("addCorrectURL", func(t *testing.T) {
		withTestEnv(
			func() {
				rpcClient := NewAria2cRPCClient()
				taskGID, err := rpcClient.AddURI("http://github.com")
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("taskGID:%s", taskGID)
			})
	})
}

func TestAria2cRPCClient_TellStatus(t *testing.T) {
	t.Run("TellHTTPStatus", func(t *testing.T) {
		withTestEnv(
			func() {
				rpcClient := NewAria2cRPCClient()
				taskGID, err := rpcClient.AddURI("https://github.com/hashicorp/consul/archive/v0.9.3.tar.gz")
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("taskGID:%s", taskGID)
				for i := 0; i < 10; i++ {
					time.Sleep(time.Microsecond * 200)
					resp, err := rpcClient.TellStatus(taskGID)
					if err != nil {
						t.Fatal(err)
					}
					t.Logf("status:%+v", resp)
				}
			})
	})
	t.Run("TellMagnetStatus", func(t *testing.T) {
		withTestEnv(
			func() {
				rpcClient := NewAria2cRPCClient()
				taskGID, err := rpcClient.AddURI("magnet:?xt=urn:btih:09c4beba230a770051207d07a8fb76cf43477523")
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("taskGID:%s", taskGID)
				for i := 0; i < 10; i++ {
					time.Sleep(time.Microsecond * 200)
					resp, err := rpcClient.TellStatus(taskGID)
					if err != nil {
						t.Fatal(err)
					}
					t.Logf("status:%+v", resp)
				}
			})
	})
}

func TestAria2cRPCClient_RemoveDownloadResult(t *testing.T) {
	withTestEnv(
		func() {
			rpcClient := NewAria2cRPCClient()
			taskGID, err := rpcClient.AddURI("http://github.com")
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("taskGID:%s", taskGID)
			// wait download complete
			time.Sleep(time.Second * 5)
			err = rpcClient.RemoveDownloadResult(taskGID)
			if err != nil {
				t.Fatal(err)
			}
		})
}
