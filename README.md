[![Go Report Card](https://goreportcard.com/badge/github.com/hanjm/file_download_proxy)](https://goreportcard.com/report/github.com/hanjm/file_download_proxy)

# A self-hosted remote downloader
- supports http[s](via http.Client), magnet(via aria2 jsonrpc) and base64 string of torrent file content.
- display progress with a cool progress circular.
- HTTP Basic access authentication (optional).
it will be useful if you have a vps.

# improving log
 - 2017-10   : refactor all golang code, more idiomatic, more modular, less global variable, less sync.Mutex.
 - 2017-03   : improved, use fix num of download worker, received task from global channel.
 - 2017-03-17: improved, use websocket server pushing instead of ajax client polling.

# live demo
<http://23.83.230.242/file_download_proxy/>

# how to use
- in docker

```shell
docker build -t fdp:latest https://raw.githubusercontent.com/hanjm/file_download_proxy/master/Dockerfile
docker run -it -p 8080:8080 -v `pwd`/download:/go/src/github.com/hanjm/file_download_proxy/download fdp ./fdp -addr ${your public ip:8080} -limit 100
```

- in vps/mac/linux

```shell
go get -v github.com/hanjm/file_download_proxy/...
git clone https://github.com/hanjm/file_download_proxy.git
cd file_download_proxy
go build -o fdp && nohup ./fdp > fdp.log 2>&1 &
```

then open `http://127.0.0.1:8000/file_download_proxy/`

# custom option
`./fdp -h`
```
  -addr string
        api addr for frontend request (default "127.0.0.1:8080")
  -aria2cPort int
        the command-line-arguments 'rpc-listen-port' when start aria2c (default 6902)
  -auth string
        http basic access authentication, username:password
  -dir string
        download dir (default "download")
  -limit int
        the limit size of download file, unit is 'GB' (default 5)
  -port int
        service listen port (default 8080)
  -timeout int
        the limit time for finish download task, unit is 'Hour' (default 48)
```