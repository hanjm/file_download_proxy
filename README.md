# file_download_proxy
文件下载代理服务
------------
- 使用Golang net/http 包实现
- 显示下载速度 css进度环

Demo:http://23.83.230.242/file_download_proxy/
```shell
Usage: go run file_download_proxy.go addr:port

Example:go run file_download_proxy.go 127.0.0.1:8000
```

update:
增加Content-Length<3G限制
https://github.com/hanjm/file_download_proxy/blob/master/file_download_proxy.go#L202-L205