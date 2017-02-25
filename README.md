# download_proxy
文件下载中转服务
------------
- 使用 Golang net/http 包实现
- 调用 Aria2c RPC 接口
- 显示下载速度 css 进度环
- 支持类型: http/磁力链接(via aria2 jsonrpc interface)

Demo:http://23.83.230.242/file_download_proxy/

# 如何使用?
## 安装

    go get github.com/weaming/download_proxy

源码安装：

	go generate && go install

`go generate` 是为了将静态文件打包到`bindata.go`

## 服务启动

```shell
Usage: download_proxy addr:port

Example: download_proxy 127.0.0.1:8000
```

然后访问: http://127.0.0.1:8000/proxy/

推荐使用tmux实现后台运行

# 注意事项
1. 增加Content-Length< 3G限制
https://github.com/hanjm/file_download_proxy/blob/master/file_download_proxy.go#L23
2. 建议拖回本地时使用迅雷/Folx等专业下载工具以达到最大速度
