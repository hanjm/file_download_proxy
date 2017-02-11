# file_download_proxy
文件下载代理服务
------------
- 使用Golang net/http 包实现
- 显示下载速度 css进度环
- 支持类型: http/磁力链接(via aria2 jsonrpc interface)
- Content-Length<3G限制 https://github.com/hanjm/file_download_proxy/blob/master/file_download_proxy.go#L23

Demo:http://23.83.230.242/file_download_proxy/

```shell
Usage: go run file_download_proxy.go addr:port

Example:go run file_download_proxy.go 127.0.0.1:8000
```
推荐使用tmux实现后台运行 https://tmux.github.io/
