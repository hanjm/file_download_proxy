package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/hanjm/log"
	"io"
	"io/ioutil"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// iana say port 6902-6934 Unassigned, it may be safety
// https://www.iana.org/assignments/service-names-port-numbers/service-names-port-numbers.xhtml
var aria2cPort = flag.Int("aria2cPort", 6902, "the command-line-arguments 'rpc-listen-port' when start aria2c")

// json rpc client
type Aria2cRPCClient struct {
	httpClient *http.Client
	requestURL string
}

func NewAria2cRPCClient() *Aria2cRPCClient {
	reqURL := fmt.Sprintf("http://127.0.0.1:%d/jsonrpc", *aria2cPort)
	return &Aria2cRPCClient{
		httpClient: &http.Client{
			Timeout: time.Minute,
		},
		requestURL: reqURL,
	}
}

func (c *Aria2cRPCClient) AddURI(uri string) (taskGID string, err error) {
	var respResult string
	return respResult, c.callAria2cAndUnmarshal("aria2.addUri", uri, []interface{}{[]string{uri}}, &respResult)
}

func (c *Aria2cRPCClient) AddTorrent(base64Content string) (taskGID string, err error) {
	var respResult string
	return respResult, c.callAria2cAndUnmarshal("aria2.addTorrent", "addTorrent", []interface{}{base64Content}, &respResult)
}

type Aria2cTellStatusResult struct {
	CompletedLength int64 `json:"completedLength,string"`
	Connections     int   `json:"connections,string"`
	DownloadSpeed   int64 `json:"downloadSpeed,string"`
	Files           []struct {
		CompletedLength int64               `json:"completedLength,string"`
		Index           int                 `json:"index,string"`
		Length          int64               `json:"length,string"`
		Path            string              `json:"path"`
		Selected        bool                `json:"selected,string"`
		URIs            []map[string]string `json:"uris"`
	} `json:"files"`
	FollowedBy   []string `json:"followedBy"`
	Following    string   `json:"following"`
	GID          string   `json:"gid"`
	NumSeeders   int      `json:"numSeeders,string"`
	Seeder       bool     `json:"seeder,string"`
	Status       string   `json:"status"`
	TotalLength  int64    `json:"totalLength,string"`
	UploadLength int64    `json:"uploadLength,string"`
	UploadSpeed  int64    `json:"uploadSpeed,string"`
}

func (r *Aria2cTellStatusResult) GetFilePath() string {
	for _, v := range r.Files {
		return v.Path
	}
	return ""
}

func (r *Aria2cTellStatusResult) Completed() bool {
	return r.Status == "complete"
}

func (c *Aria2cRPCClient) TellStatus(taskGID string) (*Aria2cTellStatusResult, error) {
	var respResult = Aria2cTellStatusResult{}
	return &respResult, c.callAria2cAndUnmarshal("aria2.tellStatus", taskGID, []interface{}{taskGID}, &respResult)
}

func (c *Aria2cRPCClient) RemoveDownloadResult(taskGID string) error {
	var respResult string
	err := c.callAria2cAndUnmarshal("aria2.removeDownloadResult", taskGID, []interface{}{taskGID}, &respResult)
	if err != nil {
		return err
	}
	if respResult != "OK" {
		return fmt.Errorf("result expect 'ok', not %s", respResult)
	}
	return nil
}

func (c *Aria2cRPCClient) callAria2cAndUnmarshal(method string, requestID string, params []interface{}, respResult interface{}) (err error) {
	var rpcReq = struct {
		Method  string        `json:"method"`
		JSONRPC string        `json:"jsonrpc"`
		ID      string        `json:"id"`
		Params  []interface{} `json:"params"`
	}{
		Method:  method,
		JSONRPC: "2.0",
		ID:      requestID,
		Params:  params,
	}
	reqData, err := json.Marshal(&rpcReq)
	if err != nil {
		err = fmt.Errorf("[callAria2c]marshal rpc req to json error:%s", err)
		return err
	}
	var resp *http.Response
	const maxRetry = 3
	for retry := 1; retry <= maxRetry; retry++ {
		resp, err = c.httpClient.Post(c.requestURL, "application/json-rpc", bytes.NewReader(reqData))
		if err != nil {
			err = fmt.Errorf("[callAria2c]do request error:%s, is aria2c process running? ", err)
			log.Warnf("%s, retry... %d/%d", err, retry, maxRetry)
			time.Sleep(time.Second)
		} else {
			break
		}
	}
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respData, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		err = fmt.Errorf("[callAria2c]read rpc resp error:%s", err)
		return err
	}
	var rpcResp = struct {
		ID      string          `json:"id"`
		JSONRPC string          `json:"jsonrpc"`
		Result  json.RawMessage `json:"result"`
		Error   struct {
			Code    int64  `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{}
	err = json.Unmarshal(respData, &rpcResp)
	if err != nil {
		err = fmt.Errorf("[callAria2c]json.Unmarshal respData error:%s, rawBody:%s", err, respData)
		return err
	}
	if rpcResp.Error.Code != 0 {
		return fmt.Errorf("[callAria2c]aria2 return error, code:%d, message:%s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	//log.Debugf("[Aria2cTellStatusResult]rpcResp.Result:%s", rpcResp.Result)
	err = json.Unmarshal(rpcResp.Result, respResult)
	if err != nil {
		err = fmt.Errorf("[callAria2c]json.Unmarshal rpcResp.Resul error:%s, rawBody:%s", err, respData)
		return err
	}
	return nil
}

var isAria2cRunning bool

func IsAria2cRunning() bool {
	return isAria2cRunning
}

func hasAria2c() bool {
	output, _ := exec.Command("hash", "aria2c").Output()
	if len(output) == 0 {
		return true
	}
	return false
}

func Aria2Worker(downloadDir string) (pid int) {
	if hasAria2c() {
		killCmd := exec.Command("sh")
		killCmd.Stdin = strings.NewReader(fmt.Sprintf(`lsof -i :%d|grep LISTEN|awk '{printf $2"\n"}'|xargs -I {} kill -9 {}`, *aria2cPort))
		err := killCmd.Run()
		if err != nil {
			log.Warnf("kill error:%s", err)
		}
		cmd := exec.Command("aria2c",
			"--dir="+downloadDir,
			"--enable-rpc",
			fmt.Sprintf("--rpc-listen-port=%d", *aria2cPort),
			"--rpc-listen-all=false",
			// https://github.com/ngosang/trackerslist
			"--bt-tracker=udp://tracker.skyts.net:6969/announce,udp://tracker.safe.moe:6969/announce,udp://tracker.piratepublic.com:1337/announce,udp://tracker.pirateparty.gr:6969/announce,udp://tracker.coppersurfer.tk:6969/announce,udp://tracker.leechers-paradise.org:6969/announce,udp://allesanddro.de:1337/announce,udp://9.rarbg.com:2710/announce,http://p4p.arenabg.com:1337/announce,udp://p4p.arenabg.com:1337/announce,udp://tracker.opentrackr.org:1337/announce,http://tracker.opentrackr.org:1337/announce,udp://public.popcorn-tracker.org:6969/announce,udp://tracker2.christianbro.pw:6969/announce,udp://tracker1.xku.tv:6969/announce,udp://tracker1.wasabii.com.tw:6969/announce,udp://tracker.zer0day.to:1337/announce,udp://tracker.mg64.net:6969/announce,udp://peerfect.org:6969/announce,udp://open.facedatabg.net:6969/announc")
		output, err := cmd.StdoutPipe()
		if err != nil {
			log.Errorf("[Aria2Worker]cmd.StdoutPipe error:%s", err)
		}
		err = cmd.Start()
		isAria2cRunning = true
		if err != nil {
			log.Errorf("[Aria2Worker]aria2c can not start, err:%s", err.Error())
			isAria2cRunning = false
		}
		go func(output io.ReadCloser) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Errorf("[Aria2Worker]panic:%v", rec)
				}
			}()
			// log aria2c stdout
			scanner := bufio.NewScanner(output)
			var outString string
			for scanner.Scan() {
				// 不能让空输出刷屏
				outString = scanner.Text()
				if strings.TrimSpace(outString) != "" {
					log.Debugf("[aria2c][stdout]%s", outString)
				}
			}
			cmd.Wait()
		}(output)
		return cmd.Process.Pid
	} else {
		log.Errorf("[Aria2Worker]aria2c not install, cannot download magnet")
	}
	return 0
}
