package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"os/exec"
)

//aria2c 配置
const ARIA2_ADD_URL_METHOD = "aria2.addUri"
const ARIA2_TELL_STATUS_METHOD = "aria2.tellStatus"
const ARIA2_REMOVE_DOWNLOAD_RESULT = "aria2.removeDownloadResult"

var is_aria2c_running bool

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

func has_aria2c() bool {
	output, _ := exec.Command("hash", "aria2c").Output()
	if len(output) == 0 {
		return true
	}
	return false
}

func rpc_call_aria2c(method string, id string, params []interface{}) (*Aria2JsonRPCResp, error) {
	var response Aria2JsonRPCResp
	rpc_request, err := json.Marshal(Aria2JsonRPCReq{Method: method, Jsonrpc: "2.0", Id: id, Params: params})
	if err != nil {
		log.Printf("json marshal error %v %s\n", err, rpc_request)
		return &response, err
	}
	rpc_response, err := http.Post("http://127.0.0.1:6900/jsonrpc", "application/json-rpc", bytes.NewReader(rpc_request))
	if err != nil {
		log.Println("jsonrpc call error", err.Error())
		return &response, err
	}
	defer rpc_response.Body.Close()
	rpc_body, err := ioutil.ReadAll(rpc_response.Body)
	if err != nil {
		log.Println("jsonrpc response read error", err.Error())
		return &response, err
	}
	err = json.Unmarshal(rpc_body, &response)
	if err != nil {
		log.Printf("json unmarshal error %v %s\n", err, rpc_body)
		return &response, err
	}
	return &response, err
}
