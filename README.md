[![Go Report Card](https://goreportcard.com/badge/github.com/hanjm/file_download_proxy)](https://goreportcard.com/report/github.com/hanjm/file_download_proxy)

# A self-hosted remote downloader
it supports http[s](via http.Client), magnet(via aria2 jsonrpc) and base64 string of torrent file content.
it will display progress with a cool progress circular.
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
just run `./fdp -h` see more command line arguments.