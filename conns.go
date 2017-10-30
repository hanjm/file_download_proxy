package main

import (
	"github.com/gorilla/websocket"
	"github.com/hanjm/log"
	"sync"
	"time"
)

// manager websocket connections
type ConnectionsManger struct {
	mutex *sync.Mutex
	// key: conn value:send message channel
	conns map[*websocket.Conn]chan interface{}
}

func NewConnectionsManger() *ConnectionsManger {
	return &ConnectionsManger{
		mutex: new(sync.Mutex),
		conns: make(map[*websocket.Conn]chan interface{}, 100),
	}
}

func (cm *ConnectionsManger) Add(conn *websocket.Conn) {
	// add to map
	sendChan := make(chan interface{}, 100)
	cm.mutex.Lock()
	cm.conns[conn] = sendChan
	cm.mutex.Unlock()
	// set close handler
	conn.SetCloseHandler(func(code int, text string) error {
		// stand loseHandler
		message := []byte{}
		if code != websocket.CloseNoStatusReceived {
			message = websocket.FormatCloseMessage(code, "")
		}
		conn.WriteControl(websocket.CloseMessage, message, time.Now().Add(time.Second))
		// extra
		cm.delete(conn)
		return nil
	})
	// prepare write message
	go func() {
		var (
			msgObj       interface{}
			isNewMessage bool
			err          error
		)
		// 首条消息, 立即发送
		msgObj = <-sendChan
		conn.WriteJSON(msgObj)
		log.Debugf("first message to %s", conn.RemoteAddr())
		for {
			// 之后的消息, 限速: 取出多条消息, 只推最新一条消息
			isNewMessage = false
			for {
				select {
				case msgObj = <-sendChan:
					//log.Debugf("从sendChan取出msgObj")
					isNewMessage = true
				case <-time.After(time.Second):
					//log.Debugf("从sendChan取出msgObj, timeout.")
					goto SEND
				}
			}
		SEND:
			if !isNewMessage {
				//log.Debugf("不是新消息, 不推送.")
				continue
			}
			log.Debugf("send message to %s", conn.RemoteAddr())
			err = conn.WriteJSON(msgObj)
			if err != nil {
				err2 := conn.Close()
				if err2 != nil {
					log.Errorf("connection %s close error:%s", conn.RemoteAddr(), err2)
				}
				log.Infof("connection %s closed, number of active connections %d", conn.RemoteAddr(), len(cm.conns)-1)
				cm.delete(conn)
				return
			}
		}
	}()
}

func (cm *ConnectionsManger) delete(conn *websocket.Conn) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	delete(cm.conns, conn)
}

func (cm *ConnectionsManger) Broadcast(msgObj interface{}) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	for _, ch := range cm.conns {
		select {
		case ch <- msgObj:
		default:
		}
	}
}

func (cm *ConnectionsManger) Count() int {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	return len(cm.conns)
}
