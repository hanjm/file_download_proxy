# 文件下载代理/离线下载服务
- 使用Golang net/http包实现
- 显示下载速度 css进度环
- 支持类型: http[s](via http.Client)/磁力链接(via aria2 jsonrpc interface)
- 2017-03改进: 文件下载函数复用Goroutines,代替原来的直接go func
- 2017-03-17更新: 使用channel + websocket实现仅当有文件在下载时服务端推送文件状态更新,代替原来消耗过大的客户端ajax轮询.

Demo:http://23.83.230.242/file_download_proxy/

# 如何使用
服务启动代码如下：
```shell
go get github.com/gorilla/websocket

Usage: go run main.go addr:port
Example: go run main.go 127.0.0.1:8000
```
然后访问: http://127.0.0.1:8000/file_download_proxy/

推荐使用 supervisor 或 tmux 后台运行 https://tmux.github.io/

# 注意事项
1. Content-Length < 3G 限制
https://github.com/hanjm/file_download_proxy/blob/master/main.go#L28
2. testfile 限制
https://github.com/hanjm/file_download_proxy/blob/master/main.go#L42
3. 建议拖回本地时使用迅雷/Folx等专业下载工具以达到最大速度,下载GitHub的大资源只需要粘贴源地址,不要粘贴重定向到AWS的地址
